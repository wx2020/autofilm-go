package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	logger   *logrus.Logger
	loggerMu sync.RWMutex
)

// InitLogger 初始化日志系统
func InitLogger() {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	settings := GetSettings()

	logger = logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetFormatter(&logrus.TextFormatter{
		DisableColors:    false,
		FullTimestamp:    true,
		TimestampFormat:  "2006-01-02 15:04:05",
		ForceColors:      true,
		DisableQuote:     true,
	})

	// 设置日志级别
	if settings.IsDebug() {
		logger.SetLevel(logrus.DebugLevel)
	} else {
		logger.SetLevel(logrus.InfoLevel)
	}

	// 控制台输出
	consoleFormatter := &CustomFormatter{
		ForceColors: true,
	}
	consoleHook := NewConsoleHook(consoleFormatter)
	logger.AddHook(consoleHook)

	// 文件输出
	logFile := settings.GetLogFile()
	fileHook := NewFileHook(logFile)
	logger.AddHook(fileHook)
}

// GetLogger 获取日志记录器
func GetLogger() *logrus.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return logger
}

// CustomFormatter 自定义日志格式化器
type CustomFormatter struct {
	ForceColors bool
}

// Format 格式化日志
func (f *CustomFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	// 格式：【级别】时间 | 消息
	timestamp := entry.Time.Format("2006-01-02 15:04:05")
	message := fmt.Sprintf("【%s】%s | %s\n",
		entry.Level.String(),
		timestamp,
		entry.Message)
	return []byte(message), nil
}

// ConsoleHook 控制台日志钩子
type ConsoleHook struct {
	formatter *CustomFormatter
}

// NewConsoleHook 创建控制台日志钩子
func NewConsoleHook(formatter *CustomFormatter) *ConsoleHook {
	return &ConsoleHook{formatter: formatter}
}

// Fire 触发日志
func (hook *ConsoleHook) Fire(entry *logrus.Entry) error {
	line, err := hook.formatter.Format(entry)
	if err != nil {
		return err
	}

	// 根据日志级别设置颜色
	color := getColorByLevel(entry.Level)
	os.Stdout.Write([]byte(color))
	os.Stdout.Write(line)
	resetColor()
	return nil
}

// Levels 返回支持的日志级别
func (hook *ConsoleHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// FileHook 文件日志钩子
type FileHook struct {
	basePath  string        // 日志文件基本路径（不含日期）
	currentDate string      // 当前日志文件的日期
	file       *os.File     // 当前日志文件句柄
	formatter  *CustomFormatter
	mu         sync.Mutex   // 保护并发访问
}

// NewFileHook 创建文件日志钩子
func NewFileHook(basePath string) *FileHook {
	// 确保日志目录存在
	os.MkdirAll(filepath.Dir(basePath), 0755)

	hook := &FileHook{
		basePath:  basePath,
		formatter: &CustomFormatter{ForceColors: false},
	}

	// 初始化日志文件
	hook.rotateFile()

	return hook
}

// rotateFile 切换日志文件
func (hook *FileHook) rotateFile() {
	currentDate := time.Now().Format("2006-01-02")

	// 如果日期未变化，无需切换
	if hook.currentDate == currentDate && hook.file != nil {
		return
	}

	// 关闭旧文件
	if hook.file != nil {
		hook.file.Close()
	}

	// 构建新的日志文件名：AutoFilm-2026-02-23.log
	dir := filepath.Dir(hook.basePath)
	baseName := filepath.Base(hook.basePath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]

	newFileName := fmt.Sprintf("%s-%s%s", nameWithoutExt, currentDate, ext)
	newFilePath := filepath.Join(dir, newFileName)

	// 打开新文件
	file, err := os.OpenFile(newFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// 如果打开失败，尝试使用原路径
		file, err = os.OpenFile(hook.basePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
	}

	hook.file = file
	hook.currentDate = currentDate
}

// Fire 触发日志
func (hook *FileHook) Fire(entry *logrus.Entry) error {
	hook.mu.Lock()
	defer hook.mu.Unlock()

	// 检查是否需要切换日志文件
	hook.rotateFile()

	if hook.file == nil {
		return nil
	}

	line, err := hook.formatter.Format(entry)
	if err != nil {
		return err
	}
	_, err = hook.file.Write(line)
	return err
}

// Levels 返回支持的日志级别
func (hook *FileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Close 关闭文件钩子
func (hook *FileHook) Close() error {
	hook.mu.Lock()
	defer hook.mu.Unlock()

	if hook.file != nil {
		return hook.file.Close()
	}
	return nil
}

// ANSI 颜色代码
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
)

func getColorByLevel(level logrus.Level) string {
	switch level {
	case logrus.DebugLevel:
		return colorBlue
	case logrus.InfoLevel:
		return colorGreen
	case logrus.WarnLevel:
		return colorYellow
	case logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel:
		return colorRed
	default:
		return colorReset
	}
}

func resetColor() {
	os.Stdout.Write([]byte(colorReset))
}
