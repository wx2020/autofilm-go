package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
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
	debug     bool
	timezone  string
	viper     *viper.Viper
	viperMu   sync.Mutex  // viper 非线程安全，需加锁串行访问
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
	} else {
		// 开发环境
		sm.configDir = "config"
		sm.logDir = "logs"
		sm.cacheDir = filepath.Join("config", "cache")
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
}

// loadConfig 加载配置文件
func (sm *SettingManager) loadConfig() {
	configFile := filepath.Join(sm.configDir, "config.yaml")
	sm.viper.SetConfigFile(configFile)
	sm.viper.SetConfigType("yaml")

	// 读取配置文件
	if err := sm.viper.ReadInConfig(); err != nil {
		// viper 在使用 SetConfigFile 模式下，文件不存在时返回 *os.PathError，
		// 而非 viper.ConfigFileNotFoundError（后者仅在未设置具体路径时返回）。
		// 直接以 os.Stat 兜底检查：文件不存在则创建默认配置。
		if _, statErr := os.Stat(configFile); os.IsNotExist(statErr) {
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
  #   enable: true                      # 是否启用此条目（可选，默认 true，调试时设为 false 即可临时禁用而不删除条目）
  #   run_on_start: false               # 启动时立即执行一次，不等 cron（可选，默认 false）
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
  #   scan_mode: "incremental"          # 扫描模式: full（全量）| incremental（增量），默认 incremental
  #   qps_limit: 10                     # QPS 限制，0 表示自动计算（max_workers/2，最大 10）
  #   cron: "0 */6 * * *"

Ani2AlistList: []
  # - id: "example"
  #   enable: true                      # 是否启用此条目（可选，默认 true）
  #   run_on_start: false               # 启动时立即执行一次，不等 cron（可选，默认 false）
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
  #   enable: true                      # 是否启用此条目（可选，默认 true）
  #   run_on_start: false               # 启动时立即执行一次，不等 cron（可选，默认 false）
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

// GetCacheDir 获取缓存目录
func (sm *SettingManager) GetCacheDir() string {
	return sm.cacheDir
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
	var result []map[string]interface{}
	sm.viper.UnmarshalKey("Alist2StrmList", &result)
	return result
}

// GetAni2AlistList 获取 Ani2Alist 列表
func (sm *SettingManager) GetAni2AlistList() []map[string]interface{} {
	var result []map[string]interface{}
	sm.viper.UnmarshalKey("Ani2AlistList", &result)
	return result
}

// GetLibraryPosterList 获取 LibraryPoster 列表
func (sm *SettingManager) GetLibraryPosterList() []map[string]interface{} {
	var result []map[string]interface{}
	sm.viper.UnmarshalKey("LibraryPosterList", &result)
	return result
}

// ReloadConfig 重新加载配置文件
// viper 非线程安全，调用前需持锁以确保并发安全。
func (sm *SettingManager) ReloadConfig() error {
	sm.viperMu.Lock()
	defer sm.viperMu.Unlock()
	return sm.viper.ReadInConfig()
}

// OnConfigChange 注册配置文件变更回调。
// 内部启用 viper.WatchConfig()（基于 fsnotify 或回退轮询），
// 每次配置文件写入事件触发时调用 callback。
// 仅支持注册一个回调；多次调用会以最后一次为准（与 viper.OnConfigChange 行为一致）。
func (sm *SettingManager) OnConfigChange(callback func()) {
	sm.viper.WatchConfig()
	sm.viper.OnConfigChange(func(_ fsnotify.Event) {
		callback()
	})
}
