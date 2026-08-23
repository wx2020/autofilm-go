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
	dataDir   string
	debug     bool
	timezone  string
	viper     *viper.Viper
	viperMu   sync.Mutex // viper 非线程安全，需加锁串行访问
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

// createDefaultConfig 创建默认配置文件
func (sm *SettingManager) createDefaultConfig() {
	configFile := filepath.Join(sm.configDir, "config.yaml")

	// 默认配置模板
	defaultConfig := `Settings:
  DEV: false
  TZ: Asia/Shanghai
  Web:
    Enabled: true
    Host: 127.0.0.1
    Port: 8080
    Token: ""

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

AlistSyncList: []
  # - id: "cloud-a-to-b"
  #   enable: true
  #   run_on_start: false
  #   url: "http://localhost:5244"
  #   username: ""
  #   password: ""
  #   token: ""
  #   pairs:
  #     - src: "/aliyun/Movies"
  #       dst: "/backup/Movies"
  #       delete_src: false
  #       overwrite: "if_newer"          # never | always | if_newer
  #   retry:
  #     max_attempts: 10
  #     backoff: "expo"
  #     jitter: 0.2
  #   wait_time: 0
  #   cron: "0 */2 * * *"

FileMoveList: []
  # - id: "local-movie-archive"
  #   enable: true
  #   run_on_start: false
  #   source_dir: "D:/downloads"
  #   target_dir: "D:/media"
  #   regex: "(?i)\\.(mkv|mp4)$"
  #   size: null
  #   min_size: 0
  #   max_size: 0
  #   overwrite: false
  #   cron: "0 */10 * * * *"
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

// GetAlistSyncList 获取 AlistSync 同步列表
func (sm *SettingManager) GetAlistSyncList() []map[string]interface{} {
	var result []map[string]interface{}
	sm.viper.UnmarshalKey("AlistSyncList", &result)
	return result
}

func (sm *SettingManager) GetFileMoveList() []map[string]interface{} {
	var result []map[string]interface{}
	sm.viper.UnmarshalKey("FileMoveList", &result)
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
