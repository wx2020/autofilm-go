package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/viper"
)

var (
	// Version can be set at build time using: -ldflags "-X main.Version=X.Y.Z"
	Version = "v1.5.0-1"
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
	} else {
		// 开发环境
		sm.configDir = "config"
		sm.logDir = "logs"
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
}

// loadConfig 加载配置文件
func (sm *SettingManager) loadConfig() {
	configFile := filepath.Join(sm.configDir, "config.yaml")
	sm.viper.SetConfigFile(configFile)
	sm.viper.SetConfigType("yaml")

	// 读取配置文件
	if err := sm.viper.ReadInConfig(); err != nil {
		// 如果配置文件不存在，创建默认配置
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			sm.createDefaultConfig()
		} else {
			fmt.Printf("Error reading config file: %v\n", err)
		}
	}

	// 加载设置
	sm.debug = sm.viper.GetBool("Settings.DEV")
	sm.timezone = sm.viper.GetString("Settings.TZ")
	if sm.timezone == "" {
		sm.timezone = DefaultTZ
	}
}

// createDefaultConfig 创建默认配置文件
func (sm *SettingManager) createDefaultConfig() {
	configFile := filepath.Join(sm.configDir, "config.yaml")

	// 默认配置模板
	defaultConfig := `Settings:
  DEV: false
  TZ: Asia/Shanghai

Alist2StrmList: []
  # - id: "example"
  #   url: "http://localhost:5244"
  #   username: ""
  #   password: ""
  #   token: ""
  #   public_url: ""
  #   source_dir: "/"
  #   target_dir: "/media"
  #   flatten_mode: false
  #   subtitle: false
  #   image: false
  #   nfo: false
  #   mode: "AlistURL"
  #   overwrite: false
  #   other_ext: ""
  #   max_workers: 50
  #   max_downloaders: 5
  #   wait_time: 0
  #   sync_server: false
  #   sync_ignore: ""
  #   smart_protection:
  #     enabled: false
  #     threshold: 100
  #     grace_scans: 3
  #   cron: "0 */6 * * *"

Ani2AlistList: []
  # - id: "example"
  #   url: "http://localhost:5244"
  #   username: ""
  #   password: ""
  #   token: ""
  #   target_dir: "/Anime"
  #   rss_update: true
  #   year: null
  #   month: null
  #   src_domain: "aniopen.an-i.workers.dev"
  #   rss_domain: "api.ani.rip"
  #   key_word: ""
  #   cron: "0 */12 * * *"

LibraryPosterList: []
  # - id: "example"
  #   url: "http://localhost:8096"
  #   api_key: ""
  #   title_font_path: "/fonts/title.ttf"
  #   subtitle_font_path: "/fonts/subtitle.ttf"
  #   configs:
  #     - library_name: "Movies"
  #       title: "电影"
  #       subtitle: "Movie Library"
  #       limit: 15
  #   cron: "0 4 * * *"
`

	if err := os.WriteFile(configFile, []byte(defaultConfig), 0644); err != nil {
		fmt.Printf("Error creating default config: %v\n", err)
	}
}

// GetConfigDir 获取配置文件目录
func (sm *SettingManager) GetConfigDir() string {
	return sm.configDir
}

// GetLogDir 获取日志文件目录
func (sm *SettingManager) GetLogDir() string {
	return sm.logDir
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

// GetAlistServerList 获取 Alist2Strm 服务器列表
func (sm *SettingManager) GetAlistServerList() []map[string]interface{} {
	return sm.viper.Get("Alist2StrmList").([]map[string]interface{})
}

// GetAni2AlistList 获取 Ani2Alist 列表
func (sm *SettingManager) GetAni2AlistList() []map[string]interface{} {
	return sm.viper.Get("Ani2AlistList").([]map[string]interface{})
}

// GetLibraryPosterList 获取 LibraryPoster 列表
func (sm *SettingManager) GetLibraryPosterList() []map[string]interface{} {
	return sm.viper.Get("LibraryPosterList").([]map[string]interface{})
}

// ReloadConfig 重新加载配置文件
func (sm *SettingManager) ReloadConfig() error {
	return sm.viper.ReadInConfig()
}
