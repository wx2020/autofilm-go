package core

import "sync"

var (
	reloadCh     chan struct{}
	reloadOnce   sync.Once
)

// ReloadCh 返回一个全局配置重载信号 channel
// 当配置通过 Web API 保存后，向此 channel 发送信号以通知 main 重建 cron
func ReloadCh() <-chan struct{} {
	reloadOnce.Do(func() {
		reloadCh = make(chan struct{}, 1)
	})
	return reloadCh
}

// TriggerReload 触发配置重载信号（非阻塞发送）
func TriggerReload() {
	reloadOnce.Do(func() {
		reloadCh = make(chan struct{}, 1)
	})
	select {
	case reloadCh <- struct{}{}:
	default:
	}
}
