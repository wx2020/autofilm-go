package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

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
	file     *os.File
	formatter *CustomFormatter
}

// NewFileHook 创建文件日志钩子
func NewFileHook(filename string) *FileHook {
	// 确保日志目录存在
	os.MkdirAll(filepath.Dir(filename), 0755)

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}

	return &FileHook{
		file:     file,
		formatter: &CustomFormatter{ForceColors: false},
	}
}

// Fire 触发日志
func (hook *FileHook) Fire(entry *logrus.Entry) error {
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
