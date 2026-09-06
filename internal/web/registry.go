package web

import (
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// ModuleType 模块类型
type ModuleType string

const (
	ModuleAlist2Strm    ModuleType = "alist2strm"
	ModuleAni2Alist     ModuleType = "ani2alist"
	ModuleLibraryPoster ModuleType = "libraryposter"
	ModuleAlistSync     ModuleType = "alistsync"
	ModuleFileMove      ModuleType = "filemove"
)

// ModuleEntry 模块条目运行时状态
type ModuleEntry struct {
	Type      ModuleType `json:"type"`
	ID        string     `json:"id"`
	Enabled   bool       `json:"enabled"`
	Cron      string     `json:"cron"`
	NextRun   time.Time  `json:"next_run"`
	LastRun   time.Time  `json:"last_run"`
	LastError string     `json:"last_error"`
	Running   bool       `json:"running"`
	RunFunc   func()     `json:"-"`
}

// ModuleRegistry 模块注册表
// 并发约定：所有对 entry 字段的读取都通过返回副本完成，
// 写操作（Register/SetEnabled/执行锁）持写锁，避免读写竞争。
// NextRun 在每次读取时新鲜计算（副本内），注册表内不长期保存，
// 避免零值/陈旧时间被展示为 1/1/1。
type ModuleRegistry struct {
	mu      sync.RWMutex
	entries map[string]*ModuleEntry
	running map[string]bool // 正在执行的任务（type:id）：同一任务同时只允许跑一个
}

// sharedCronParser 全局统一的 cron 解析器：秒字段可选（兼容 5 段与 6 段
// 表达式），同时支持 @daily/@every 等描述符。
// 注册表、cron 调度器、WebUI 校验必须使用同一份，避免“校验通过但排期
// 失败”或“排期成功但下次运行显示 1/1/1”。
var sharedCronParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// SharedCronParser 返回全局统一的 cron 解析器
func SharedCronParser() cron.Parser {
	return sharedCronParser
}

// NewModuleRegistry 创建模块注册表
func NewModuleRegistry() *ModuleRegistry {
	return &ModuleRegistry{
		entries: make(map[string]*ModuleEntry),
	}
}

func (r *ModuleRegistry) key(typ ModuleType, id string) string {
	return string(typ) + ":" + id
}

// computeNextRun 纯函数：根据 cron 计算下次运行时间（不修改 entry）
// cron 为空、模块禁用或表达式非法时返回零时间，调用方应将其隐藏而非展示
func computeNextRun(cronSpec string, enabled bool) time.Time {
	if cronSpec == "" || !enabled {
		return time.Time{}
	}
	schedule, err := sharedCronParser.Parse(cronSpec)
	if err != nil {
		return time.Time{}
	}
	return schedule.Next(time.Now())
}

// Register 注册模块
func (r *ModuleRegistry) Register(entry *ModuleEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *entry
	r.entries[r.key(entry.Type, entry.ID)] = &stored
}

// Unregister 注销模块
func (r *ModuleRegistry) Unregister(typ ModuleType, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, r.key(typ, id))
}

// Get 获取模块副本（返回 NextRun 新鲜计算的副本，避免外部修改共享状态）
func (r *ModuleRegistry) Get(typ ModuleType, id string) *ModuleEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[r.key(typ, id)]
	if !ok {
		return nil
	}
	e := *entry
	e.NextRun = computeNextRun(e.Cron, e.Enabled)
	e.Running = r.running[r.key(e.Type, e.ID)]
	return &e
}

// IsEnabled 判断模块当前是否启用
func (r *ModuleRegistry) IsEnabled(typ ModuleType, id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[r.key(typ, id)]
	if !ok {
		return false
	}
	return entry.Enabled
}

// SetEnabled 更新模块启用状态（加锁写入），返回是否存在该模块
func (r *ModuleRegistry) SetEnabled(typ ModuleType, id string, enabled bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[r.key(typ, id)]
	if !ok {
		return false
	}
	entry.Enabled = enabled
	return true
}

// TryAcquireRun 原子抢占某任务的执行权：当前无运行中任务时返回 true 并
// 标记为运行中；已有任务在跑时返回 false（调用方应拒绝重复触发）。
func (r *ModuleRegistry) TryAcquireRun(typ ModuleType, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(typ, id)
	if r.running[k] {
		return false
	}
	if r.running == nil {
		r.running = make(map[string]bool)
	}
	r.running[k] = true
	return true
}

// ReleaseRun 释放某任务的执行权（任务结束时调用，与 TryAcquireRun 配对）
func (r *ModuleRegistry) ReleaseRun(typ ModuleType, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, r.key(typ, id))
}

// IsRunning 判断某任务当前是否有运行中的实例
func (r *ModuleRegistry) IsRunning(typ ModuleType, id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running[r.key(typ, id)]
}

// List 列出所有模块（返回 NextRun/Running 新鲜计算的副本）
func (r *ModuleRegistry) List() []*ModuleEntry {
	r.mu.RLock()
	result := make([]*ModuleEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		e := *entry
		e.NextRun = computeNextRun(e.Cron, e.Enabled)
		e.Running = r.running[r.key(e.Type, e.ID)]
		result = append(result, &e)
	}
	r.mu.RUnlock()
	return result
}

// ListByType 按类型列出模块（返回 NextRun/Running 新鲜计算的副本）
func (r *ModuleRegistry) ListByType(typ ModuleType) []*ModuleEntry {
	r.mu.RLock()
	var result []*ModuleEntry
	for _, entry := range r.entries {
		if entry.Type == typ {
			e := *entry
			e.NextRun = computeNextRun(e.Cron, e.Enabled)
			e.Running = r.running[r.key(e.Type, e.ID)]
			result = append(result, &e)
		}
	}
	r.mu.RUnlock()
	return result
}

// Clear 清空注册表（配置重载时调用）
// 注意：running 执行锁不清空——运行中的任务 goroutine 仍然存活，清空会
// 导致按钮误判为可运行从而重复触发；锁由任务结束时的 ReleaseRun 释放。
func (r *ModuleRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[string]*ModuleEntry)
}

var globalRegistry *ModuleRegistry

func init() {
	globalRegistry = NewModuleRegistry()
}

// GetModuleRegistry 获取全局模块注册表
func GetModuleRegistry() *ModuleRegistry {
	return globalRegistry
}
