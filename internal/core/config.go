package core

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/viper"
)

var (
	// Version can be set at build time using: -ldflags "-X github.com/akimio/autofilm/internal/core.Version=X.Y.Z"
	Version = "dev"
)

const (
	AppName   = "AutoFilm"
	DefaultTZ = "Asia/Shanghai"
)

// AppVersion returns the current version
func AppVersion() string {
	return Version
}

var (
	instance *SettingManager
	once     sync.Once
)

// SettingManager 系统配置管理器
type SettingManager struct {
	configDir string
	logDir    string
	cacheDir  string
	dataDir   string
	debug     bool
	timezone  string
	viper     *viper.Viper
}

// GetSettings 获取配置管理器单例
func GetSettings() *SettingManager {
	once.Do(func() {
		instance = &SettingManager{
			viper: viper.New(),
		}
		instance.init()
	})
	return instance
}

// init 初始化配置管理器
func (sm *SettingManager) init() {
	// 获取可执行文件所在目录
	if exePath, err := os.Executable(); err == nil {
		sm.configDir = filepath.Join(filepath.Dir(exePath), "config")
		sm.logDir = filepath.Join(filepath.Dir(exePath), "logs")
		sm.cacheDir = filepath.Join(filepath.Dir(exePath), "config", "cache")
		sm.dataDir = filepath.Join(filepath.Dir(exePath), "data")
	} else {
		// 开发环境
		sm.configDir = "config"
		sm.logDir = "logs"
		sm.cacheDir = filepath.Join("config", "cache")
		sm.dataDir = "data"
	}

	// 创建必要的目录
	sm.mkdir()

	// 加载配置
	sm.loadConfig()
}

// mkdir 创建必要的目录
func (sm *SettingManager) mkdir() {
	os.MkdirAll(sm.configDir, 0755)
	os.MkdirAll(sm.logDir, 0755)
	os.MkdirAll(sm.cacheDir, 0755)
	os.MkdirAll(sm.dataDir, 0755)
}

// loadConfig 加载配置文件
func (sm *SettingManager) loadConfig() {
	// 系统与模块配置保存在 SQLite。环境变量仅用于首次启动覆盖。
	_ = sm.viper.BindEnv("Settings.Web.Enabled", "AUTOFILM_WEB_ENABLED")
	_ = sm.viper.BindEnv("Settings.Web.Host", "AUTOFILM_WEB_HOST")
	_ = sm.viper.BindEnv("Settings.Web.Port", "AUTOFILM_WEB_PORT")
	_ = sm.viper.BindEnv("Settings.Web.Token", "AUTOFILM_WEB_TOKEN")

	sm.viper.SetDefault("Settings.DEV", false)
	sm.viper.SetDefault("Settings.TZ", DefaultTZ)
	sm.viper.SetDefault("Settings.Web.Enabled", true)
	sm.viper.SetDefault("Settings.Web.Host", "127.0.0.1")
	sm.viper.SetDefault("Settings.Web.Port", 8080)
	sm.debug = sm.viper.GetBool("Settings.DEV")
	sm.timezone = sm.viper.GetString("Settings.TZ")
	if sm.timezone == "" {
		sm.timezone = DefaultTZ
	}
}

// createDefaultConfig 创建默认配置文件（已废弃：配置以 SQLite 为准，保留空实现避免外部引用）
func (sm *SettingManager) createDefaultConfig() {}

// GetConfigDir 获取配置文件目录
func (sm *SettingManager) GetConfigDir() string {
	return sm.configDir
}

// GetLogDir 获取日志文件目录
func (sm *SettingManager) GetLogDir() string {
	return sm.logDir
}

// GetCacheDir 获取缓存目录
func (sm *SettingManager) GetCacheDir() string {
	return sm.cacheDir
}

// GetDataDir 获取数据目录（存放 autofilm.db 等）
func (sm *SettingManager) GetDataDir() string {
	return sm.dataDir
}

// GetConfigFile 获取配置文件路径
func (sm *SettingManager) GetConfigFile() string {
	return filepath.Join(sm.configDir, "config.yaml")
}

// GetLogFile 获取日志文件路径
func (sm *SettingManager) GetLogFile() string {
	if sm.debug {
		return filepath.Join(sm.logDir, "dev.log")
	}
	return filepath.Join(sm.logDir, "AutoFilm.log")
}

// IsDebug 是否为调试模式
func (sm *SettingManager) IsDebug() bool {
	return sm.debug
}

// GetTimezone 获取时区
func (sm *SettingManager) GetTimezone() string {
	return sm.timezone
}

// ApplyRuntimeSettings applies values loaded from SQLite before logger/tasks start.
func (sm *SettingManager) ApplyRuntimeSettings(debug bool, timezone string) {
	sm.debug = debug
	if timezone == "" {
		timezone = DefaultTZ
	}
	sm.timezone = timezone
}

// GetWebEnabled 返回是否启动 Web 管理服务。
func (sm *SettingManager) GetWebEnabled() bool {
	return sm.viper.GetBool("Settings.Web.Enabled")
}

// GetWebHost 返回 Web 服务监听地址。
func (sm *SettingManager) GetWebHost() string {
	return sm.viper.GetString("Settings.Web.Host")
}

// GetWebPort 返回 Web 服务监听端口。
func (sm *SettingManager) GetWebPort() int {
	return sm.viper.GetInt("Settings.Web.Port")
}

// GetWebToken 返回 API Bearer Token；为空时不启用鉴权。
func (sm *SettingManager) GetWebToken() string {
	return sm.viper.GetString("Settings.Web.Token")
}
