package alist2strm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/extensions"
	"github.com/akimio/autofilm/pkg/alist"
	"github.com/akimio/autofilm/pkg/httpclient"
	"github.com/sirupsen/logrus"
)

// Config Alist2Strm配置
type Config struct {
	ID             string
	URL            string
	Username       string
	Password       string
	Token          string
	PublicURL      string
	SourceDir      string
	TargetDir      string
	FlattenMode    bool
	Subtitle       bool
	Image          bool
	NFO            bool
	Mode           string
	Overwrite      bool
	OtherExt       string
	MaxWorkers     int
	MaxDownloaders int
	WaitTime       float64
	SyncServer     bool
	SyncIgnore     string
	SmartProtection *SmartProtectionConfig
	Cron           string
}

// SmartProtectionConfig 智能保护配置
type SmartProtectionConfig struct {
	Enabled    bool  `json:"enabled"`
	Threshold  int   `json:"threshold"`
	GraceScans int   `json:"grace_scans"`
}

// Alist2Strm Alist转STRM处理器
type Alist2Strm struct {
	config           *Config
	client           *alist.AlistClient
	mode             Alist2StrmMode
	processFileExts  map[string]bool
	downloadExts     map[string]bool
	protection       *StrmProtectionManager
	bdmvManager      *BDMVManager
	processedPaths   map[string]struct{}
	processedMu      sync.RWMutex
	logger           *logrus.Logger
}

// New 创建新的Alist2Strm实例
func New(cfg *Config) (*Alist2Strm, error) {
	client, err := alist.GetClient(cfg.URL, cfg.Username, cfg.Password, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("创建Alist客户端失败: %w", err)
	}

	// 处理public_url
	publicURL := cfg.PublicURL
	if publicURL != "" && !strings.HasPrefix(publicURL, "http") {
		publicURL = "https://" + publicURL
	}
	publicURL = strings.TrimRight(publicURL, "/")

	// 解析自定义扩展名
	otherExts := parseOtherExts(cfg.OtherExt)

	// 获取需要处理的文件扩展名
	processExts := extensions.GetProcessFileExts(cfg.Subtitle, cfg.Image, cfg.NFO, otherExts)
	downloadExts := extensions.GetDownloadExts(cfg.Subtitle, cfg.Image, cfg.NFO, otherExts)

	// 平铺模式下禁用下载
	if cfg.FlattenMode {
		downloadExts = make(map[string]bool)
	}

	a2s := &Alist2Strm{
		config:          cfg,
		client:          client,
		mode:            FromStr(cfg.Mode),
		processFileExts: processExts,
		downloadExts:    downloadExts,
		bdmvManager:     NewBDMVManager(),
		processedPaths:  make(map[string]struct{}),
		logger:          core.GetLogger(),
	}

	// 初始化智能保护
	if cfg.SmartProtection != nil && cfg.SmartProtection.Enabled {
		a2s.protection = NewStrmProtectionManager(
			cfg.TargetDir,
			cfg.ID,
			cfg.SmartProtection.Threshold,
			cfg.SmartProtection.GraceScans,
		)
		if err := a2s.protection.Load(); err != nil {
			a2s.logger.Warnf("加载保护状态失败: %v", err)
		}
		a2s.logger.Infof(".strm保护已启用：阈值=%d，宽限期=%d",
			cfg.SmartProtection.Threshold, cfg.SmartProtection.GraceScans)
	}

	return a2s, nil
}

// Run 运行Alist2Strm处理
func (a2s *Alist2Strm) Run(ctx context.Context) error {
	a2s.logger.Info("开始Alist2Strm处理")

	waitTime := time.Duration(a2s.config.WaitTime) * time.Second

	// 创建worker池
	maxWorkers := a2s.config.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 50
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)
	pathCh := make(chan *alist.AlistPath, maxWorkers*2)

	// 启动文件处理worker
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range pathCh {
				sem <- struct{}{}
				a2s.processFile(ctx, path)
				<-sem
			}
		}()
	}

	// 过滤器函数
	filterFunc := func(path *alist.AlistPath) bool {
		return a2s.shouldProcessFile(path)
	}

	// 第一阶段：遍历并处理普通文件
	outCh, errCh := a2s.client.IterPath(ctx, a2s.config.SourceDir, waitTime, a2s.mode == RawURLMode, filterFunc)

	for {
		select {
		case path, ok := <-outCh:
			if !ok {
				goto Done
			}
			pathCh <- path

		case err, ok := <-errCh:
			if !ok {
				goto Done
			}
			a2s.logger.Errorf("遍历路径出错: %v", err)

		case <-ctx.Done():
			close(pathCh)
			wg.Wait()
			return ctx.Err()
		}
	}

Done:
	close(pathCh)
	wg.Wait()

	// 完成BDMV文件收集
	a2s.bdmvManager.Finalize()

	// 第二阶段：处理BDMV最大文件
	for _, largestFile := range a2s.bdmvManager.GetLargestFiles() {
		a2s.logger.Infof("处理BDMV文件: %s (%.1f MB)",
			largestFile.Name, float64(largestFile.Size)/1024/1024)

		// 重新获取详细信息
		var fileToProcess *alist.AlistPath
		if a2s.mode == RawURLMode && largestFile.RawURL == "" {
			detailed, err := a2s.client.FSGet(ctx, largestFile.FullPath)
			if err != nil {
				a2s.logger.Warnf("重新获取BDMV文件详细信息失败: %v", err)
				fileToProcess = largestFile
			} else {
				fileToProcess = detailed
			}
		} else {
			fileToProcess = largestFile
		}

		a2s.processFile(ctx, fileToProcess)

		// 记录已处理路径
		localPath := a2s.getLocalPath(fileToProcess)
		a2s.addProcessedPath(localPath)
	}

	// 保存保护状态
	if a2s.protection != nil {
		if err := a2s.protection.Save(); err != nil {
			a2s.logger.Errorf("保存保护状态失败: %v", err)
		}
	}

	// 同步服务器（清理本地文件）
	if a2s.config.SyncServer {
		if err := a2s.cleanupLocalFiles(ctx); err != nil {
			a2s.logger.Errorf("清理本地文件失败: %v", err)
		} else {
			a2s.logger.Info("清理过期的.strm文件完成")
		}
	}

	a2s.logger.Info("Alist2Strm处理完成")
	return nil
}

// shouldProcessFile 判断是否应该处理该文件
func (a2s *Alist2Strm) shouldProcessFile(path *alist.AlistPath) bool {
	// 跳过目录
	if path.IsDir() {
		return false
	}

	// 跳过系统文件
	skipFolders := []string{"@eaDir", "Thumbs.db", ".DS_Store"}
	for _, folder := range skipFolders {
		if strings.Contains(path.FullPath, folder) {
			return false
		}
	}

	// 检查是否为BDMV文件
	if IsBDMVFile(path) {
		a2s.bdmvManager.CollectFile(path)
		return false // BDMV文件稍后单独处理
	}

	// 检查文件扩展名
	if !a2s.processFileExts[path.Suffix()] {
		return false
	}

	// 获取本地路径
	localPath := a2s.getLocalPath(path)
	a2s.addProcessedPath(localPath)

	// 检查文件是否已存在
	if !a2s.config.Overwrite {
		if fileInfo, err := os.Stat(localPath); err == nil {
			if a2s.downloadExts[path.Suffix()] {
				// 对于下载文件，检查修改时间和大小
				modTime := path.ModifiedTimestamp()
				if modTime > 0 && fileInfo.ModTime().Unix() < modTime {
					return true // 文件已过期
				}
				if fileInfo.Size() < path.Size {
					return true // 文件大小不一致
				}
			}
			return false // 文件已存在且不需要覆盖
		}
	}

	return true
}

// processFile 处理单个文件
func (a2s *Alist2Strm) processFile(ctx context.Context, path *alist.AlistPath) {
	localPath := a2s.getLocalPath(path)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		a2s.logger.Errorf("创建目录失败: %v", err)
		return
	}

	// 生成内容
	content := a2s.generateContent(path)
	if content == "" {
		a2s.logger.Warnf("文件 %s 的内容为空，跳过处理", path.FullPath)
		return
	}

	// 判断是创建.strm文件还是下载文件
	if filepath.Ext(localPath) == ".strm" {
		if err := os.WriteFile(localPath, []byte(content), 0644); err != nil {
			a2s.logger.Errorf("创建.strm文件失败: %v", err)
			return
		}
		a2s.logger.Infof("%s 创建成功", filepath.Base(localPath))
	} else {
		// 下载文件
		if err := a2s.downloadFile(ctx, path.RawURL, localPath); err != nil {
			a2s.logger.Errorf("下载文件失败: %v", err)
			return
		}
		a2s.logger.Infof("%s 下载成功", filepath.Base(localPath))
	}
}

// generateContent 生成文件内容
func (a2s *Alist2Strm) generateContent(path *alist.AlistPath) string {
	switch a2s.mode {
	case AlistURLMode:
		content := path.RawURL
		if a2s.config.PublicURL != "" {
			content = strings.Replace(content, a2s.config.URL, a2s.config.PublicURL, 1)
		}
		return content

	case RawURLMode:
		return path.RawURL

	case AlistPathMode:
		return path.FullPath

	default:
		return path.RawURL
	}
}

// downloadFile 下载文件
func (a2s *Alist2Strm) downloadFile(ctx context.Context, url, filePath string) error {
	client := httpclient.GetClient()
	return client.Download(ctx, url, filePath, nil)
}

// getLocalPath 获取本地文件路径
func (a2s *Alist2Strm) getLocalPath(path *alist.AlistPath) string {
	if a2s.config.FlattenMode {
		return filepath.Join(a2s.config.TargetDir, path.Name)
	}

	// 计算相对路径
	relPath := strings.TrimPrefix(path.FullPath, a2s.config.SourceDir)
	if strings.HasPrefix(relPath, "/") {
		relPath = relPath[1:]
	}

	localPath := filepath.Join(a2s.config.TargetDir, relPath)

	// 视频文件转换为.strm
	if extensions.IsVideoExt(path.Suffix()) {
		localPath = localPath[:len(localPath)-len(path.Suffix())] + ".strm"
	}

	return localPath
}

// cleanupLocalFiles 清理本地已删除的文件
func (a2s *Alist2Strm) cleanupLocalFiles(ctx context.Context) error {
	a2s.logger.Info("开始清理本地文件")

	var allLocalFiles []string
	var err error

	if a2s.config.FlattenMode {
		files, err := os.ReadDir(a2s.config.TargetDir)
		if err != nil {
			return err
		}
		for _, f := range files {
			if !f.IsDir() {
				allLocalFiles = append(allLocalFiles, filepath.Join(a2s.config.TargetDir, f.Name()))
			}
		}
	} else {
		err = filepath.Walk(a2s.config.TargetDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				allLocalFiles = append(allLocalFiles, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	// 找出需要删除的文件
	filesToDelete := make(map[string]struct{})
	a2s.processedMu.RLock()
	for _, file := range allLocalFiles {
		if _, exists := a2s.processedPaths[file]; !exists {
			filesToDelete[file] = struct{}{}
		}
	}
	a2s.processedMu.RUnlock()

	// 分离.strm和其他文件
	strmToDelete := make(map[string]struct{})
	otherToDelete := make(map[string]struct{})

	for file := range filesToDelete {
		if filepath.Ext(file) == ".strm" {
			strmToDelete[file] = struct{}{}
		} else {
			otherToDelete[file] = struct{}{}
		}
	}

	// 应用智能保护
	if a2s.protection != nil {
		// 转换为集合
		strmPresent := make(map[string]struct{})
		a2s.processedMu.RLock()
		for path := range a2s.processedPaths {
			if filepath.Ext(path) == ".strm" {
				strmPresent[path] = struct{}{}
			}
		}
		a2s.processedMu.RUnlock()

		strmToDelete = a2s.protection.Process(strmToDelete, strmPresent)
	}

	// 合并待删除文件
	for file := range otherToDelete {
		strmToDelete[file] = struct{}{}
	}

	// 检查同步忽略模式
	var syncIgnorePattern *regexp.Regexp
	if a2s.config.SyncIgnore != "" {
		syncIgnorePattern = regexp.MustCompile(a2s.config.SyncIgnore)
	}

	// 执行删除
	for file := range filesToDelete {
		// 检查忽略模式
		if syncIgnorePattern != nil && syncIgnorePattern.MatchString(filepath.Base(file)) {
			a2s.logger.Debugf("文件 %s 在忽略列表中，跳过删除", filepath.Base(file))
			continue
		}

		if err := os.Remove(file); err != nil {
			a2s.logger.Errorf("删除文件失败: %v", err)
		} else {
			a2s.logger.Infof("删除文件: %s", file)
		}

		// 删除空目录
		a2s.removeEmptyDirs(filepath.Dir(file))
	}

	return nil
}

// removeEmptyDirs 递归删除空目录
func (a2s *Alist2Strm) removeEmptyDirs(dir string) {
	if dir == a2s.config.TargetDir {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	if len(entries) == 0 {
		os.Remove(dir)
		a2s.logger.Infof("删除空目录: %s", dir)
		a2s.removeEmptyDirs(filepath.Dir(dir))
	}
}

// addProcessedPath 添加已处理路径
func (a2s *Alist2Strm) addProcessedPath(path string) {
	a2s.processedMu.Lock()
	defer a2s.processedMu.Unlock()
	a2s.processedPaths[path] = struct{}{}
}

// parseOtherExts 解析自定义扩展名
func parseOtherExts(otherExts string) []string {
	if otherExts == "" {
		return nil
	}

	parts := strings.Split(otherExts, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		ext := strings.TrimSpace(strings.ToLower(part))
		if ext != "" {
			result = append(result, ext)
		}
	}
	return result
}
