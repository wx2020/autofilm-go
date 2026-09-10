package filemove

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/pkg/alist"
	"github.com/sirupsen/logrus"
)

// Config describes one recursive local file move task.
// Regex is matched against the slash-separated path relative to SourceDir.
// Size, when set, is an exact size in bytes. A zero MinSize or MaxSize means
// that the corresponding bound is not set.
type Config struct {
	ID                string
	Enable            bool
	RunOnStart        bool
	SourceDir         string
	TargetDir         string
	Regex             string
	Size              *int64
	MinSize           int64
	MaxSize           int64
	Overwrite         bool
	Flatten           bool
	RenameRegex       string
	RenameReplacement string
	RemoveMatchedDirs bool
	Cron              string
	Backend           string
	URL               string
	Username          string
	Password          string
	Token             string
	QPSLimit          int // OpenList API 限流（次/秒），0 表示不限流；仅 openlist 后端生效
}

// ParseSize parses a byte size such as 1073741824, 50KB, 500MB or 1GB.
// Units use binary multiples: 1KB=1024 bytes.
func ParseSize(value interface{}) (int64, error) {
	if value == nil {
		return 0, errors.New("size 为空")
	}
	if number, ok := value.(float64); ok {
		if number < 0 || math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) || number >= 9223372036854775808 {
			return 0, fmt.Errorf("无效的数字大小 %v", number)
		}
		return int64(number), nil
	}
	if number, ok := value.(int64); ok {
		if number < 0 {
			return 0, fmt.Errorf("无效的数字大小 %d", number)
		}
		return number, nil
	}
	if number, ok := value.(int); ok {
		if number < 0 {
			return 0, fmt.Errorf("无效的数字大小 %d", number)
		}
		return int64(number), nil
	}
	text := strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
	if text == "" {
		return 0, errors.New("size 为空")
	}
	match := regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*(B|KB|MB|GB|TB)?$`).FindStringSubmatch(text)
	if match == nil {
		return 0, fmt.Errorf("无效的大小 %q；请使用字节数或 B/KB/MB/GB/TB", text)
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("无效的大小 %q", text)
	}
	multiplier := float64(1)
	switch match[2] {
	case "KB":
		multiplier = 1 << 10
	case "MB":
		multiplier = 1 << 20
	case "GB":
		multiplier = 1 << 30
	case "TB":
		multiplier = 1 << 40
	}
	bytes := number * multiplier
	if bytes < 0 || bytes != math.Trunc(bytes) || bytes >= 9223372036854775808 {
		return 0, fmt.Errorf("大小 %q 超出支持的字节范围", text)
	}
	return int64(bytes), nil
}

// MoveReport contains the result of one scan.
type MoveReport struct {
	Scanned     int
	Matched     int
	Moved       int
	Renamed     int
	Skipped     int
	RemovedDirs int
	Errors      []error
}

// Error returns all per-file errors as one single-line error.
// 错误间用分号连接（而非 errors.Join 的换行），保证一条日志只占一行，
// 日志级别过滤与按行展示不会把续行漏掉或拆散。
func (r MoveReport) Error() error {
	if len(r.Errors) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(r.Errors))
	for _, err := range r.Errors {
		if err != nil {
			msgs = append(msgs, err.Error())
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return errors.New(strings.Join(msgs, "; "))
}

// FileMover recursively moves files matching its configuration.
type FileMover struct {
	config    Config
	sourceDir string
	targetDir string
	pattern   *regexp.Regexp
	rename    *regexp.Regexp
	client    *alist.AlistClient
	logger    *logrus.Logger
}

// infof 与 alist2strm 对齐：所有日志自动带任务 ID 前缀
func (m *FileMover) infof(format string, args ...interface{}) {
	if m.logger == nil {
		return
	}
	m.logger.Infof("[%s] %s", m.config.ID, fmt.Sprintf(format, args...))
}

// New validates a file move configuration and creates a mover.
func New(cfg *Config) (*FileMover, error) {
	if cfg == nil {
		return nil, errors.New("filemove 配置为空")
	}
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = "local"
	}
	if backend != "local" && backend != "openlist" {
		return nil, fmt.Errorf("不支持的 filemove 后端: %s", cfg.Backend)
	}
	if strings.TrimSpace(cfg.SourceDir) == "" {
		return nil, errors.New("source_dir 不能为空")
	}
	if strings.TrimSpace(cfg.TargetDir) == "" {
		return nil, errors.New("target_dir 不能为空")
	}
	if cfg.Size != nil && *cfg.Size < 0 {
		return nil, errors.New("size 不能为负数")
	}
	if cfg.MinSize < 0 || cfg.MaxSize < 0 {
		return nil, errors.New("min_size 和 max_size 不能为负数")
	}
	if cfg.MaxSize > 0 && cfg.MinSize > cfg.MaxSize {
		return nil, errors.New("min_size 不能大于 max_size")
	}

	sourceDir, err := filepath.Abs(filepath.Clean(cfg.SourceDir))
	if err != nil {
		return nil, fmt.Errorf("解析 source_dir 失败: %w", err)
	}
	targetDir, err := filepath.Abs(filepath.Clean(cfg.TargetDir))
	if err != nil {
		return nil, fmt.Errorf("解析 target_dir 失败: %w", err)
	}
	if backend == "local" && samePath(sourceDir, targetDir) {
		return nil, errors.New("source_dir 和 target_dir 不能相同")
	}
	if backend == "local" && pathWithin(sourceDir, targetDir) {
		return nil, errors.New("target_dir 不能位于 source_dir 内部")
	}
	if backend == "openlist" {
		if !strings.HasPrefix(cfg.SourceDir, "/") || !strings.HasPrefix(cfg.TargetDir, "/") {
			return nil, errors.New("openlist 的 source_dir 和 target_dir 必须以 / 开头")
		}
		if cleanRemotePath(cfg.SourceDir) == cleanRemotePath(cfg.TargetDir) {
			return nil, errors.New("source_dir 和 target_dir 不能相同")
		}
		if remotePathWithin(cfg.SourceDir, cfg.TargetDir) {
			return nil, errors.New("target_dir 不能位于 source_dir 内部")
		}
	}

	var pattern *regexp.Regexp
	if strings.TrimSpace(cfg.Regex) != "" {
		pattern, err = regexp.Compile(cfg.Regex)
		if err != nil {
			return nil, fmt.Errorf("编译 regex 失败: %w", err)
		}
	}
	var renamePattern *regexp.Regexp
	if strings.TrimSpace(cfg.RenameRegex) != "" {
		renamePattern, err = regexp.Compile(cfg.RenameRegex)
		if err != nil {
			return nil, fmt.Errorf("编译 rename_regex 失败: %w", err)
		}
	}

	mover := &FileMover{
		config:    *cfg,
		sourceDir: sourceDir,
		targetDir: targetDir,
		pattern:   pattern,
		rename:    renamePattern,
		logger:    core.GetLogger(),
	}
	if backend == "openlist" {
		client, err := alist.GetClient(cfg.URL, cfg.Username, cfg.Password, cfg.Token)
		if err != nil {
			return nil, fmt.Errorf("创建 OpenList 客户端失败: %w", err)
		}
		// 声明本任务的限流策略（0 表示不限流），共享客户端时后启动者生效
		client.SetRateLimit(cfg.QPSLimit)
		mover.client = client
	}
	return mover, nil
}

// Move scans and moves matching files until the scan completes or ctx is
// cancelled. A non-nil error means the scan itself failed or one or more
// files could not be moved; successful files are still reflected in report.
func (m *FileMover) Move(ctx context.Context) (MoveReport, error) {
	m.infof("开始FileMove扫描")
	if m.client != nil {
		return m.moveOpenList(ctx)
	}
	var report MoveReport
	matchedDirs := map[string]int{}
	movedDirs := map[string]int{}

	if err := ctx.Err(); err != nil {
		return report, err
	}
	info, err := os.Stat(m.sourceDir)
	if err != nil {
		return report, fmt.Errorf("读取 source_dir 状态失败: %w", err)
	}
	if !info.IsDir() {
		return report, fmt.Errorf("source_dir 不是目录: %s", m.sourceDir)
	}
	if err := os.MkdirAll(m.targetDir, 0755); err != nil {
		return report, fmt.Errorf("创建 target_dir 失败: %w", err)
	}

	err = filepath.WalkDir(m.sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			report.Errors = append(report.Errors, fmt.Errorf("遍历 %s 失败: %w", path, walkErr))
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == m.sourceDir || entry.IsDir() {
			return nil
		}
		// WalkDir does not follow directory symlinks, but explicitly skip all
		// symlinks so a file symlink is never copied as if it were a real file.
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}

		report.Scanned++
		if m.rename != nil {
			oldName := entry.Name()
			newName := m.rename.ReplaceAllString(oldName, m.config.RenameReplacement)
			if newName != "" && newName != oldName {
				renamed := filepath.Join(filepath.Dir(path), newName)
				if _, err := os.Lstat(renamed); err == nil {
					if !m.config.Overwrite {
						report.Skipped++
						return nil
					}
					if err := os.Remove(renamed); err != nil {
						report.Errors = append(report.Errors, fmt.Errorf("删除已存在的重命名目标 %s 失败: %w", renamed, err))
						return nil
					}
				}
				if err := os.Rename(path, renamed); err != nil {
					report.Errors = append(report.Errors, fmt.Errorf("重命名 %s 为 %s 失败: %w", path, renamed, err))
					return nil
				}
				report.Renamed++
				path = renamed
			}
		}
		fileInfo, err := os.Stat(path)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("读取文件状态 %s 失败: %w", path, err))
			return nil
		}
		rel, err := filepath.Rel(m.sourceDir, path)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("计算相对路径 %s 失败: %w", path, err))
			return nil
		}
		if !m.matches(filepath.ToSlash(rel), fileInfo.Size()) {
			return nil
		}
		report.Matched++
		matchedDirs[filepath.Dir(path)]++

		destinationRel := rel
		if m.config.Flatten {
			destinationRel = filepath.Base(rel)
		}
		destination := filepath.Join(m.targetDir, destinationRel)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("创建父目录 %s 失败: %w", destination, err))
			return nil
		}
		if err := moveFile(path, destination, fileInfo, m.config.Overwrite); err != nil {
			if errors.Is(err, fs.ErrExist) {
				report.Skipped++
			} else {
				report.Errors = append(report.Errors, err)
			}
			return nil
		}
		report.Moved++
		movedDirs[filepath.Dir(path)]++
		return nil
	})
	if err != nil {
		return report, err
	}
	if m.config.RemoveMatchedDirs {
		removed, cleanupErr := removeMatchedLocalDirs(m.sourceDir, matchedDirs, movedDirs)
		report.RemovedDirs += removed
		if cleanupErr != nil {
			report.Errors = append(report.Errors, cleanupErr)
		}
	}
	return report, report.Error()
}

func (m *FileMover) moveOpenList(ctx context.Context) (MoveReport, error) {
	var report MoveReport
	sourceDir := cleanRemotePath(m.config.SourceDir)
	targetDir := cleanRemotePath(m.config.TargetDir)
	files, err := listOpenListRecursive(ctx, m.client, sourceDir)
	if err != nil {
		return report, fmt.Errorf("列出 OpenList source_dir 失败: %w", err)
	}
	matchedDirs := map[string]int{}
	movedDirs := map[string]int{}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		rel := strings.TrimPrefix(file.FullPath, sourceDir+"/")
		if sourceDir == "/" {
			rel = strings.TrimPrefix(file.FullPath, "/")
		}
		report.Scanned++
		if m.rename != nil {
			newName := m.rename.ReplaceAllString(file.Name, m.config.RenameReplacement)
			if newName != "" && newName != file.Name {
				newPath := joinRemotePath(pathDir(file.FullPath), newName)
				if existing, err := m.client.FSGet(ctx, newPath); err == nil && existing != nil {
					if !m.config.Overwrite {
						report.Skipped++
						continue
					}
					if err := m.client.FSRemove(ctx, pathDir(newPath), []string{pathBase(newPath)}); err != nil {
						report.Errors = append(report.Errors, fmt.Errorf("删除已存在的重命名目标 %s 失败: %w", newPath, err))
						continue
					}
				}
				if err := m.client.FSRename(ctx, file.FullPath, newName); err != nil {
					report.Errors = append(report.Errors, fmt.Errorf("重命名 %s 为 %s 失败: %w", file.FullPath, newPath, err))
					continue
				}
				file.FullPath, file.Name, rel = newPath, newName, filepath.ToSlash(filepath.Join(filepath.Dir(rel), newName))
				report.Renamed++
			}
		}
		if !m.matches(rel, file.Size) {
			continue
		}
		report.Matched++
		matchedDirs[pathDir(file.FullPath)]++
		destinationRel := rel
		if m.config.Flatten {
			destinationRel = pathBase(rel)
		}
		destination := joinRemotePath(targetDir, destinationRel)
		if existing, err := m.client.FSGet(ctx, destination); err == nil && existing != nil {
			if !m.config.Overwrite {
				report.Skipped++
				continue
			}
			// overwrite 时先删除旧目标，避免 fs/move 撞名被服务端自动改名后误删对象
			if err := m.client.FSRemove(ctx, pathDir(destination), []string{pathBase(destination)}); err != nil {
				report.Errors = append(report.Errors, fmt.Errorf("删除已存在的目标 %s 失败: %w", destination, err))
				continue
			}
		}
		if err := m.client.FSMkdir(ctx, pathDir(destination)); err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("创建目标目录 %s 失败: %w", pathDir(destination), err))
			continue
		}
		if err := m.client.FSMove(ctx, pathDir(file.FullPath), pathDir(destination), []string{pathBase(file.FullPath)}); err != nil {
			report.Errors = append(report.Errors, fmt.Errorf("移动 %s 到 %s 失败: %w", file.FullPath, destination, err))
			continue
		}
		report.Moved++
		movedDirs[pathDir(file.FullPath)]++
	}
	if m.config.RemoveMatchedDirs {
		removed, cleanupErr := m.removeMatchedOpenListDirs(ctx, sourceDir, matchedDirs, movedDirs)
		report.RemovedDirs += removed
		if cleanupErr != nil {
			report.Errors = append(report.Errors, cleanupErr)
		}
	}
	return report, report.Error()
}

func removeMatchedLocalDirs(sourceDir string, matchedDirs, movedDirs map[string]int) (int, error) {
	paths := make([]string, 0, len(matchedDirs))
	for dir, matched := range matchedDirs {
		if matched == 0 || movedDirs[dir] != matched {
			continue
		}
		if samePath(dir, sourceDir) || !pathWithin(sourceDir, dir) {
			continue
		}
		paths = append(paths, dir)
	}
	sort.Slice(paths, func(i, j int) bool {
		return strings.Count(paths[i], string(os.PathSeparator)) > strings.Count(paths[j], string(os.PathSeparator))
	})
	removed := 0
	for _, dir := range paths {
		if err := os.RemoveAll(dir); err != nil {
			return removed, fmt.Errorf("删除已匹配的源目录 %s 失败: %w", dir, err)
		}
		removed++
	}
	return removed, nil
}

func (m *FileMover) removeMatchedOpenListDirs(ctx context.Context, sourceDir string, matchedDirs, movedDirs map[string]int) (int, error) {
	paths := make([]string, 0, len(matchedDirs))
	for dir, matched := range matchedDirs {
		if matched == 0 || movedDirs[dir] != matched {
			continue
		}
		if dir == sourceDir || !remotePathWithin(sourceDir, dir) {
			continue
		}
		paths = append(paths, dir)
	}
	sort.Slice(paths, func(i, j int) bool {
		return strings.Count(paths[i], "/") > strings.Count(paths[j], "/")
	})
	removed := 0
	for _, dir := range paths {
		if err := m.client.FSRemove(ctx, pathDir(dir), []string{pathBase(dir)}); err != nil {
			return removed, fmt.Errorf("删除已匹配的 OpenList 目录 %s 失败: %w", dir, err)
		}
		removed++
	}
	return removed, nil
}

func listOpenListRecursive(ctx context.Context, client *alist.AlistClient, dir string) ([]alist.AlistPath, error) {
	var result []alist.AlistPath
	var walk func(string) error
	walk = func(path string) error {
		entries, err := client.FSListLight(ctx, path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if err := walk(entry.FullPath); err != nil {
					return err
				}
			} else {
				result = append(result, entry)
			}
		}
		return nil
	}
	return result, walk(dir)
}

func cleanRemotePath(path string) string {
	path = "/" + strings.Trim(path, "/")
	if path == "//" {
		return "/"
	}
	return path
}

func joinRemotePath(base, rel string) string {
	return strings.TrimRight(cleanRemotePath(base), "/") + "/" + strings.TrimLeft(filepath.ToSlash(rel), "/")
}

func pathDir(path string) string {
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." {
		return "/"
	}
	return dir
}

func pathBase(path string) string { return filepath.Base(filepath.ToSlash(path)) }

func remotePathWithin(parent, child string) bool {
	parent = strings.TrimRight(cleanRemotePath(parent), "/")
	child = cleanRemotePath(child)
	if parent == "/" {
		return child != "/"
	}
	return parent != "" && strings.HasPrefix(child, parent+"/")
}

func (m *FileMover) matches(relativePath string, size int64) bool {
	if m.pattern != nil && !m.pattern.MatchString(relativePath) {
		return false
	}
	if m.config.Size != nil && size != *m.config.Size {
		return false
	}
	if m.config.MinSize > 0 && size < m.config.MinSize {
		return false
	}
	if m.config.MaxSize > 0 && size > m.config.MaxSize {
		return false
	}
	return true
}

func moveFile(source, destination string, info fs.FileInfo, overwrite bool) error {
	if !overwrite {
		if _, err := os.Lstat(destination); err == nil {
			return fmt.Errorf("目标已存在: %w: %s", fs.ErrExist, destination)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("检查目标 %s 失败: %w", destination, err)
		}
	}

	if err := os.Rename(source, destination); err == nil {
		return nil
	}

	// Rename can fail when source and target are on different filesystems.
	// Copy to a temporary file in the destination directory first, then rename
	// the completed copy so an interrupted copy never becomes the final file.
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".autofilm-move-*")
	if err != nil {
		return fmt.Errorf("创建临时目标 %s 失败: %w", destination, err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("关闭临时目标 %s 失败: %w", temporaryPath, err)
	}
	defer os.Remove(temporaryPath)

	if err := copyFile(source, temporaryPath, info); err != nil {
		return fmt.Errorf("复制 %s 到 %s 失败: %w", source, destination, err)
	}
	if overwrite {
		if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("替换目标 %s 失败: %w", destination, err)
		}
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("落盘目标 %s 失败: %w", destination, err)
	}
	if err := os.Remove(source); err != nil {
		return fmt.Errorf("复制后删除源文件 %s 失败: %w", source, err)
	}
	return nil
}

func copyFile(source, destination string, info fs.FileInfo) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Chmod(destination, info.Mode().Perm())
}

func samePath(first, second string) bool {
	if filepath.Clean(first) == filepath.Clean(second) {
		return true
	}
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(first), filepath.Clean(second))
	}
	return false
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
