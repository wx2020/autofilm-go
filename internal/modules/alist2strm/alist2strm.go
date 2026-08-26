package alist2strm

import (
	"context"
	"fmt"
	"net/url"
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
	ID              string
	Enable          bool // 是否启用此条目，缺省视为启用
	RunOnStart      bool // 启动时立即执行一次，不等 cron，缺省视为不执行
	URL             string
	Username        string
	Password        string
	Token           string
	PublicURL       string
	SourceDir       string
	TargetDir       string
	FlattenMode     bool
	Subtitle        bool
	Image           bool
	NFO             bool
	Mode            string
	Overwrite       bool
	OtherExt        string
	MaxWorkers      int
	MaxDownloaders  int
	WaitTime        float64
	SyncServer      bool
	SyncIgnore      string
	SmartProtection *SmartProtectionConfig
	Cron            string
	ScanMode        string // "full" 或 "incremental"，默认 "incremental"
	QPSLimit        int    // QPS 限制，0 表示自动计算
}

// SmartProtectionConfig 智能保护配置
type SmartProtectionConfig struct {
	Enabled    bool `json:"enabled"`
	Threshold  int  `json:"threshold"`
	GraceScans int  `json:"grace_scans"`
}

// Alist2Strm Alist转STRM处理器
type Alist2Strm struct {
	config          *Config
	client          *alist.AlistClient
	mode            Alist2StrmMode
	processFileExts map[string]bool
	downloadExts    map[string]bool
	protection      *StrmProtectionManager
	bdmvManager     *BDMVManager
	processedPaths  map[string]struct{}
	processedMu     sync.RWMutex
	downloadSem     chan struct{} // 限制并发下载文件数（与 strm 生成独立）
	logger          *logrus.Logger
	cacheDir        string
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
		downloadSem:     make(chan struct{}, maxDownloaders(cfg.MaxDownloaders)),
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

	// 初始化快照缓存目录
	a2s.cacheDir = filepath.Join(core.GetSettings().GetConfigDir(), "cache")

	return a2s, nil
}

// Run 运行Alist2Strm处理
func (a2s *Alist2Strm) Run(ctx context.Context) error {
	switch a2s.config.ScanMode {
	case "full":
		return a2s.runFull(ctx)
	default:
		return a2s.runIncremental(ctx)
	}
}

// runFull 全量扫描模式（原有 IterPath 行为）
func (a2s *Alist2Strm) runFull(ctx context.Context) error {
	a2s.logger.Info("开始Alist2Strm全量扫描")

	// 全量模式同样应用 QPS 限流，避免大目录扫描压垮服务器
	qps := a2s.calcQPS()
	if qps > 0 {
		a2s.client.SetRateLimit(qps)
		a2s.logger.Debugf("QPS限流已设置: %d", qps)
	}

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
	scanComplete := true

	for outCh != nil || errCh != nil {
		select {
		case path, ok := <-outCh:
			if !ok {
				outCh = nil
				continue
			}
			pathCh <- path

		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			scanComplete = false
			a2s.logger.Errorf("遍历路径出错: %v", err)

		case <-ctx.Done():
			close(pathCh)
			wg.Wait()
			return ctx.Err()
		}
	}
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
	if a2s.config.SyncServer && scanComplete {
		if err := a2s.cleanupLocalFiles(ctx); err != nil {
			a2s.logger.Errorf("清理本地文件失败: %v", err)
		} else {
			a2s.logger.Info("清理过期的.strm文件完成")
		}
	} else if a2s.config.SyncServer {
		a2s.logger.Warn("本次扫描未完整成功，跳过本地文件清理")
	}

	a2s.logger.Info("Alist2Strm全量扫描完成")
	return nil
}

// runIncremental 增量扫描模式
func (a2s *Alist2Strm) runIncremental(ctx context.Context) error {
	a2s.logger.Info("开始Alist2Strm增量扫描")

	// 设置 QPS 限流
	qps := a2s.calcQPS()
	if qps > 0 {
		a2s.client.SetRateLimit(qps)
		a2s.logger.Debugf("QPS限流已设置: %d", qps)
	}

	waitTime := time.Duration(a2s.config.WaitTime) * time.Second

	// 加载历史快照
	oldSnap, err := LoadSnapshot(a2s.config.ID, a2s.cacheDir)
	if err != nil {
		a2s.logger.Warnf("加载快照失败: %v，回退至全量扫描", err)
		return a2s.runFull(ctx)
	}

	// 轻量递归遍历（不调用 fs/get）
	files, err := a2s.iterPathLight(ctx, a2s.config.SourceDir, waitTime)
	if err != nil {
		a2s.logger.Errorf("增量遍历失败: %v，回退至全量扫描", err)
		return a2s.runFull(ctx)
	}

	// 构建新快照
	newSnap := BuildSnapshot(files)

	// 远端成功响应但结果为空、且存在历史快照时，判定为数据源异常：
	// 保留旧快照并跳过本轮清理，防止空列表触发全量误删
	if len(newSnap.Files) == 0 && oldSnap != nil && len(oldSnap.Files) > 0 {
		a2s.logger.Errorf("远端未返回任何文件（原快照 %d 个条目），疑似数据源异常：已保留旧快照并跳过本轮处理与清理",
			len(oldSnap.Files))
		return nil
	}

	startTime := time.Now()

	// diff 变更
	var added, modified, deleted []string
	if oldSnap != nil {
		added, modified, deleted = DiffSnapshots(oldSnap, newSnap)
		// 远端文件未变化并不代表本地输出文件仍然存在。
		// 例如用户手动删除了 .strm，此时需要重新加入处理队列。
		for i := range files {
			file := &files[i]
			oldEntry, exists := oldSnap.Files[file.FullPath]
			newEntry, present := newSnap.Files[file.FullPath]
			if !exists || !present {
				continue
			}
			if oldEntry.Size != newEntry.Size || oldEntry.Modified != newEntry.Modified {
				continue
			}
			localPath := a2s.getLocalPath(file)
			if _, err := os.Stat(localPath); os.IsNotExist(err) {
				modified = append(modified, file.FullPath)
				a2s.logger.Infof("本地输出文件不存在，重新处理: %s", localPath)
			}
		}
	} else {
		a2s.logger.Info("无历史快照，首次运行全量处理")
		for path := range newSnap.Files {
			added = append(added, path)
		}
	}

	elapsed := time.Since(startTime)
	a2s.logger.Infof("增量扫描结果: 全量=%d, 新增=%d, 修改=%d, 删除=%d, 耗时=%v",
		len(newSnap.Files), len(added), len(modified), len(deleted), elapsed)

	// 处理新增和修改的文件
	changed := append(added, modified...)
	if len(changed) > 0 {
		maxWorkers := a2s.config.MaxWorkers
		if maxWorkers <= 0 {
			maxWorkers = 50
		}

		var wg sync.WaitGroup
		sem := make(chan struct{}, maxWorkers)

		for _, fullPath := range changed {
			if ctx.Err() != nil {
				break
			}

			// 仅对变更文件调 fs/get 获取 RawURL（典型 1% 量级）
			fileDetail, err := a2s.client.FSGet(ctx, fullPath)
			if err != nil {
				a2s.logger.Warnf("获取文件详情失败 %s: %v", fullPath, err)
				continue
			}

			// 检查是否应处理该文件（含扩展名过滤、BDMV 收集）
			if !a2s.shouldProcessFile(fileDetail) {
				continue
			}

			wg.Add(1)
			p := fileDetail
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				a2s.processFile(ctx, p)
				<-sem
			}()
		}
		wg.Wait()
	}

	// 完成BDMV文件收集
	a2s.bdmvManager.Finalize()

	// 处理BDMV最大文件
	for _, largestFile := range a2s.bdmvManager.GetLargestFiles() {
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
		localPath := a2s.getLocalPath(fileToProcess)
		a2s.addProcessedPath(localPath)
	}

	// 标记快照中所有文件为已处理（供 cleanupLocalFiles 使用）
	a2s.markSnapshotProcessed(newSnap)

	// 保存快照
	if err := SaveSnapshot(a2s.config.ID, a2s.cacheDir, newSnap); err != nil {
		a2s.logger.Errorf("保存快照失败: %v", err)
	}

	// 保存保护状态
	if a2s.protection != nil {
		if err := a2s.protection.Save(); err != nil {
			a2s.logger.Errorf("保存保护状态失败: %v", err)
		}
	}

	// 同步服务器（清理本地文件，含已删除文件）
	if a2s.config.SyncServer {
		if err := a2s.cleanupLocalFiles(ctx); err != nil {
			a2s.logger.Errorf("清理本地文件失败: %v", err)
		} else {
			a2s.logger.Info("清理过期的.strm文件完成")
		}
	}

	a2s.logger.Info("Alist2Strm增量扫描完成")
	return nil
}

// calcQPS 计算 QPS 限制值
func (a2s *Alist2Strm) calcQPS() int {
	if a2s.config.QPSLimit > 0 {
		return a2s.config.QPSLimit
	}
	qps := a2s.config.MaxWorkers / 2
	if qps <= 0 {
		qps = 1
	}
	if qps > 10 {
		qps = 10
	}
	return qps
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
		// 原子写入：先写临时文件再 rename，避免中断产生损坏的 .strm
		tmpPath := localPath + ".tmp"
		if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
			a2s.logger.Errorf("创建.strm文件失败: %v", err)
			return
		}
		if err := os.Rename(tmpPath, localPath); err != nil {
			os.Remove(tmpPath)
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
		// 若 Alist 返回的原始直链已指向 public_url 的同源 host（反代直链场景），直接复用，保留其自带的签名
		// 使用 host 比较而非子串包含，避免 public_url 为空或为短域名时误命中
		if a2s.config.PublicURL != "" && sameHost(path.RawURL, a2s.config.PublicURL) {
			return path.RawURL
		}
		// 否则用 public_url 拼接 /d 直链下载路径，并附加签名（若有）
		// 对路径中的每个分段做 URL 编码，避免中文/空格导致播放器无法解析
		encoded := encodePath(path.FullPath)
		u := a2s.config.PublicURL + "/d" + encoded
		if path.Sign != "" {
			u += "?sign=" + url.QueryEscape(path.Sign)
		}
		return u

	case RawURLMode:
		return path.RawURL

	case AlistPathMode:
		return path.FullPath

	default:
		return path.RawURL
	}
}

// encodePath 对路径分段做 URL 编码，保留分隔符 /
func encodePath(p string) string {
	if p == "" {
		return p
	}
	// 保留前导 /
	leading := ""
	if strings.HasPrefix(p, "/") {
		leading = "/"
		p = strings.TrimPrefix(p, "/")
	}
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return leading + strings.Join(parts, "/")
}

// sameHost 判断 rawURL 是否指向 publicURL 的同一 host（含端口）
// 任一解析失败都返回 false，宁可走拼接分支也不误用 RawURL
func sameHost(rawURL, publicURL string) bool {
	ru, err := url.Parse(rawURL)
	if err != nil || ru.Host == "" {
		return false
	}
	pu, err := url.Parse(publicURL)
	if err != nil || pu.Host == "" {
		return false
	}
	return ru.Host == pu.Host
}

// maxDownloaders 返回合法的最大下载并发数，<=0 则取默认值 5
func maxDownloaders(n int) int {
	if n <= 0 {
		return 5
	}
	return n
}

// downloadFile 下载文件
// 通过 downloadSem 限制并发下载数，与 max_downloaders 配置项对应，避免下载挤占磁盘 IO / 上游带宽
func (a2s *Alist2Strm) downloadFile(ctx context.Context, url, filePath string) error {
	select {
	case a2s.downloadSem <- struct{}{}:
		defer func() { <-a2s.downloadSem }()
	case <-ctx.Done():
		return ctx.Err()
	}
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

// getLocalPathFromRemote 根据远端路径计算本地路径（不依赖 AlistPath 对象）
func (a2s *Alist2Strm) getLocalPathFromRemote(remotePath string) string {
	if a2s.config.FlattenMode {
		return filepath.Join(a2s.config.TargetDir, filepath.Base(remotePath))
	}

	relPath := strings.TrimPrefix(remotePath, a2s.config.SourceDir)
	if strings.HasPrefix(relPath, "/") {
		relPath = relPath[1:]
	}

	localPath := filepath.Join(a2s.config.TargetDir, relPath)

	ext := strings.ToLower(filepath.Ext(localPath))
	if extensions.IsVideoExt(ext) {
		localPath = localPath[:len(localPath)-len(ext)] + ".strm"
	}

	return localPath
}

// markSnapshotProcessed 将快照中所有文件的本地路径标记为已处理
func (a2s *Alist2Strm) markSnapshotProcessed(snap *Snapshot) {
	a2s.processedMu.Lock()
	defer a2s.processedMu.Unlock()
	for remotePath := range snap.Files {
		localPath := a2s.getLocalPathFromRemote(remotePath)
		a2s.processedPaths[localPath] = struct{}{}
	}
}

// cleanupLocalFiles 清理本地已删除的文件
func (a2s *Alist2Strm) cleanupLocalFiles(ctx context.Context) error {
	// 安全守卫：本次扫描没有从远端看到任何文件时，绝不清理本地，
	// 防止数据源异常（驱动故障、目录改名、权限变化等）导致全量误删
	a2s.processedMu.RLock()
	seen := len(a2s.processedPaths)
	a2s.processedMu.RUnlock()
	if seen == 0 {
		err := fmt.Errorf("远端扫描结果为空，为防误删已跳过本地清理，请检查 Alist/OpenList 数据源")
		a2s.logger.Error(err)
		return err
	}

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
