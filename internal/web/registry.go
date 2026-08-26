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
	RunFunc   func()     `json:"-"`
}

// ModuleRegistry 模块注册表
// 并发约定：所有对 entry 字段的读取都通过返回副本完成，
// 写操作（Register/SetEnabled）持写锁，避免读写竞争。
type ModuleRegistry struct {
	mu      sync.RWMutex
	entries map[string]*ModuleEntry
	parser  cron.Parser
}

// NewModuleRegistry 创建模块注册表
func NewModuleRegistry() *ModuleRegistry {
	return &ModuleRegistry{
		entries: make(map[string]*ModuleEntry),
		parser:  cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

func (r *ModuleRegistry) key(typ ModuleType, id string) string {
	return string(typ) + ":" + id
}

// computeNextRun 纯函数：根据 cron 计算下次运行时间（不修改 entry）
func (r *ModuleRegistry) computeNextRun(cronSpec string, enabled bool) time.Time {
	if cronSpec == "" || !enabled {
		return time.Time{}
	}
	schedule, err := r.parser.Parse(cronSpec)
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
	stored.NextRun = r.computeNextRun(entry.Cron, entry.Enabled)
	r.entries[r.key(entry.Type, entry.ID)] = &stored
}

// Unregister 注销模块
func (r *ModuleRegistry) Unregister(typ ModuleType, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, r.key(typ, id))
}

// Get 获取模块副本（返回副本以避免外部修改共享状态）
func (r *ModuleRegistry) Get(typ ModuleType, id string) *ModuleEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[r.key(typ, id)]
	if !ok {
		return nil
	}
	e := *entry
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
	entry.NextRun = r.computeNextRun(entry.Cron, enabled)
	return true
}

// List 列出所有模块（返回副本）
func (r *ModuleRegistry) List() []*ModuleEntry {
	r.mu.RLock()
	result := make([]*ModuleEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		e := *entry
		result = append(result, &e)
	}
	r.mu.RUnlock()
	return result
}

// ListByType 按类型列出模块（返回副本）
func (r *ModuleRegistry) ListByType(typ ModuleType) []*ModuleEntry {
	r.mu.RLock()
	var result []*ModuleEntry
	for _, entry := range r.entries {
		if entry.Type == typ {
			e := *entry
			result = append(result, &e)
		}
	}
	r.mu.RUnlock()
	return result
}

// Clear 清空注册表（配置重载时调用）
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
