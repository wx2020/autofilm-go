package web

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
)

// handleListModules GET /api/modules
func (s *Server) handleListModules(w http.ResponseWriter, r *http.Request) {
	registered := GetModuleRegistry().List()
	type moduleView struct {
		entry     *ModuleEntry
		createdAt time.Time
	}
	views := make([]moduleView, 0, len(registered))
	store := storage.GlobalStore()
	for _, entry := range registered {
		view := *entry
		view.LastRun = time.Time{}
		view.LastError = ""
		if store != nil {
			if run, err := store.GetLatestTaskRun(string(entry.Type), entry.ID); err == nil && run != nil {
				view.LastRun = run.StartedAt
				view.LastError = run.ErrorSummary
			}
		}
		createdAt := time.Time{}
		if store != nil {
			createdAt, _ = store.GetModuleConfigCreatedAt(string(entry.Type), entry.ID)
		}
		views = append(views, moduleView{entry: &view, createdAt: createdAt})
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].createdAt.IsZero() != views[j].createdAt.IsZero() {
			return !views[i].createdAt.IsZero()
		}
		if !views[i].createdAt.Equal(views[j].createdAt) {
			return views[i].createdAt.Before(views[j].createdAt)
		}
		if views[i].entry.Type != views[j].entry.Type {
			return views[i].entry.Type < views[j].entry.Type
		}
		return views[i].entry.ID < views[j].entry.ID
	})
	entries := make([]*ModuleEntry, 0, len(views))
	for _, view := range views {
		entries = append(entries, view.entry)
	}
	json.NewEncoder(w).Encode(entries)
}

// handleRunModule POST /api/modules/{type}/{id}/run
func (s *Server) handleRunModule(w http.ResponseWriter, r *http.Request) {
	typ := ModuleType(chi.URLParam(r, "type"))
	id := chi.URLParam(r, "id")

	entry := GetModuleRegistry().Get(typ, id)
	if entry == nil {
		http.Error(w, `{"error":"模块未找到"}`, http.StatusNotFound)
		return
	}

	if !entry.Enabled {
		http.Error(w, `{"error":"模块已禁用"}`, http.StatusBadRequest)
		return
	}

	if entry.RunFunc == nil {
		http.Error(w, `{"error":"模块运行函数未注册"}`, http.StatusInternalServerError)
		return
	}

	// 同一任务同时只允许跑一个：抢占执行权失败说明上次运行尚未结束
	if !GetModuleRegistry().TryAcquireRun(typ, id) {
		writeJSONError(w, http.StatusConflict, "任务正在运行中，请稍后再试")
		return
	}

	go func() {
		defer GetModuleRegistry().ReleaseRun(typ, id)
		// RunFunc 内部会处理错误日志
		entry.RunFunc()
	}()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "模块已触发执行",
	})
}

// handleToggleModule POST /api/modules/{type}/{id}/toggle
// 同时更新内存状态并持久化 enable 字段到 SQLite，重启/热重载后保持。
func (s *Server) handleToggleModule(w http.ResponseWriter, r *http.Request) {
	typ := ModuleType(chi.URLParam(r, "type"))
	id := chi.URLParam(r, "id")

	entry := GetModuleRegistry().Get(typ, id)
	if entry == nil {
		http.Error(w, `{"error":"模块未找到"}`, http.StatusNotFound)
		return
	}

	newEnabled := !entry.Enabled
	if !GetModuleRegistry().SetEnabled(typ, id, newEnabled) {
		http.Error(w, `{"error":"模块未找到"}`, http.StatusNotFound)
		return
	}

	if store := storage.GlobalStore(); store != nil {
		if err := store.UpdateModuleConfigField(string(typ), id, "enable", newEnabled); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "保存启用状态失败: "+err.Error())
			return
		}
		core.TriggerReload()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"enabled": newEnabled,
	})
}
