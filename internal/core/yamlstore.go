package core

import (
	"sync"

	"github.com/sirupsen/logrus"
)

// YamlStore 基于 YAML 文件的 ConfigStore 实现。
// 内部包装 SettingManager（基于 viper），复用其加载/解析/默认值逻辑，
// 并在其上增加文件变更监听与订阅广播。
type YamlStore struct {
	sm *SettingManager

	subsMu     sync.Mutex
	subscribers []chan struct{}

	logger *logrus.Logger
}

// NewYamlStore 构造 YamlStore。
// 调用方需随后调用 Init() 启动文件监听。
func NewYamlStore() *YamlStore {
	return &YamlStore{
		sm:     GetSettings(),
		logger: GetLogger(),
	}
}

// Init 启动配置文件监听。
// 监听由 viper.WatchConfig 提供（基于 fsnotify，部分环境下回退轮询）。
// 配置文件变更时自动调用 Reload 并通知所有订阅者。
func (y *YamlStore) Init() error {
	y.sm.OnConfigChange(func() {
		if err := y.Reload(); err != nil {
			y.logger.Errorf("配置变更后重载失败: %v", err)
			return
		}
		y.notify()
	})
	return nil
}

// Reload 重新加载配置文件并广播变更。
func (y *YamlStore) Reload() error {
	if err := y.sm.ReloadConfig(); err != nil {
		return err
	}
	y.notify()
	return nil
}

// Subscribe 订阅配置变更。
// 返回的 channel 容量为 1，通知为非阻塞发送（订阅者慢则本次通知被合并丢弃）。
func (y *YamlStore) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)

	y.subsMu.Lock()
	y.subscribers = append(y.subscribers, ch)
	y.subsMu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			y.subsMu.Lock()
			defer y.subsMu.Unlock()
			for i, c := range y.subscribers {
				if c == ch {
					y.subscribers = append(y.subscribers[:i], y.subscribers[i+1:]...)
					break
				}
			}
			close(ch)
		})
	}

	return ch, unsub
}

// notify 非阻塞地向所有订阅者广播一次变更。
func (y *YamlStore) notify() {
	y.subsMu.Lock()
	subs := make([]chan struct{}, len(y.subscribers))
	copy(subs, y.subscribers)
	y.subsMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// IsDebug 是否为调试模式
func (y *YamlStore) IsDebug() bool { return y.sm.IsDebug() }

// GetTimezone 获取时区
func (y *YamlStore) GetTimezone() string { return y.sm.GetTimezone() }

// GetAlist2StrmList 获取 Alist2Strm 条目列表
func (y *YamlStore) GetAlist2StrmList() []map[string]interface{} {
	return y.sm.GetAlistServerList()
}

// GetAni2AlistList 获取 Ani2Alist 条目列表
func (y *YamlStore) GetAni2AlistList() []map[string]interface{} {
	return y.sm.GetAni2AlistList()
}

// GetLibraryPosterList 获取 LibraryPoster 条目列表
func (y *YamlStore) GetLibraryPosterList() []map[string]interface{} {
	return y.sm.GetLibraryPosterList()
}

// GetAlistSyncList 获取 AlistSync 条目列表。
// P0 阶段未实现，返回空切片以满足接口。
func (y *YamlStore) GetAlistSyncList() []map[string]interface{} {
	return nil
}

// GetConfigDir 获取配置目录
func (y *YamlStore) GetConfigDir() string { return y.sm.GetConfigDir() }

// GetLogDir 获取日志目录
func (y *YamlStore) GetLogDir() string { return y.sm.GetLogDir() }

// GetConfigFile 获取配置文件路径
func (y *YamlStore) GetConfigFile() string { return y.sm.GetConfigFile() }
