package core

import (
	"sync"
)

var (
	store     ConfigStore
	storeOnce sync.Once
)

// Store 获取全局 ConfigStore 单例。
// 首次调用时按以下顺序初始化：
//  1. 触发 GetSettings()，确保配置目录与默认配置文件存在
//  2. 构造 YamlStore
//  3. 调用 Init() 启动文件监听
//
// 重复调用安全。
func Store() ConfigStore {
	storeOnce.Do(func() {
		// 确保 SettingManager 已初始化（创建配置目录与默认 yaml）
		_ = GetSettings()

		store = NewYamlStore()
		if err := store.Init(); err != nil {
			GetLogger().Warnf("ConfigStore 初始化警告: %v", err)
		}
	})
	return store
}

// SetStore 替换全局 ConfigStore 实现。
// 主要用于：
//   - P3 阶段注入 SQLStore
//   - 单元测试中注入 mock 实现
//
// 必须在 Store() 首次调用之前调用，否则不会生效。
func SetStore(s ConfigStore) {
	store = s
}
