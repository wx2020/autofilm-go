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
	ModuleAlissync      ModuleType = "alissync"
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

// Register 注册模块
func (r *ModuleRegistry) Register(entry *ModuleEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[r.key(entry.Type, entry.ID)] = entry
	entry.updateNextRun(r)
}

// Unregister 注销模块
func (r *ModuleRegistry) Unregister(typ ModuleType, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, r.key(typ, id))
}

// Get 获取模块
func (r *ModuleRegistry) Get(typ ModuleType, id string) *ModuleEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[r.key(typ, id)]
}

// List 列出所有模块
func (r *ModuleRegistry) List() []*ModuleEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*ModuleEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		entry.updateNextRun(r)
		result = append(result, entry)
	}
	return result
}

// ListByType 按类型列出模块
func (r *ModuleRegistry) ListByType(typ ModuleType) []*ModuleEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*ModuleEntry
	for _, entry := range r.entries {
		if entry.Type == typ {
			entry.updateNextRun(r)
			result = append(result, entry)
		}
	}
	return result
}

// Clear 清空注册表（配置重载时调用）
func (r *ModuleRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[string]*ModuleEntry)
}

func (e *ModuleEntry) updateNextRun(r *ModuleRegistry) {
	if e.Cron == "" || !e.Enabled {
		e.NextRun = time.Time{}
		return
	}
	schedule, err := r.parser.Parse(e.Cron)
	if err != nil {
		e.NextRun = time.Time{}
		return
	}
	e.NextRun = schedule.Next(time.Now())
}

var globalRegistry *ModuleRegistry

func init() {
	globalRegistry = NewModuleRegistry()
}

// GetModuleRegistry 获取全局模块注册表
func GetModuleRegistry() *ModuleRegistry {
	return globalRegistry
}
