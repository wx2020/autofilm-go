package core

// ConfigStore 配置存储抽象接口
//
// P0 阶段先以 YamlStore 实现；P3 阶段以 SQLStore 实现同样接口，
// 业务模块与 main.go 通过 ConfigStore 访问配置，与具体后端解耦。
type ConfigStore interface {
	// Init 初始化（加载配置、启动文件监听等）
	Init() error

	// Reload 重新加载配置，并通知所有订阅者
	Reload() error

	// Subscribe 订阅配置变更。
	// 返回一个 buffered channel（容量 1）与取消订阅函数。
	// 当配置发生变化时，channel 将收到一个空结构体（非阻塞发送，订阅者慢不会拖住通知方）。
	// 若短时间内发生多次变更，可能被合并为一次通知。
	Subscribe() (<-chan struct{}, func())

	// 系统设置
	IsDebug() bool
	GetTimezone() string

	// 模块配置列表
	// 返回 []map[string]interface{} 以兼容 YAML 后端的原始结构；
	// SQL 后端在 P3 阶段通过 JSON 序列化复用相同签名。
	GetAlist2StrmList() []map[string]interface{}
	GetAni2AlistList() []map[string]interface{}
	GetLibraryPosterList() []map[string]interface{}
	GetAlistSyncList() []map[string]interface{}

	// 路径
	GetConfigDir() string
	GetLogDir() string
	GetConfigFile() string
}
