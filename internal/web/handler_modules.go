package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
)

// handleListModules GET /api/modules
func (s *Server) handleListModules(w http.ResponseWriter, r *http.Request) {
	registered := GetModuleRegistry().List()
	entries := make([]*ModuleEntry, 0, len(registered))
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
		entries = append(entries, &view)
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

	go func() {
		// RunFunc 内部会处理错误日志
		entry.RunFunc()
	}()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "模块已触发执行",
	})
}

// handleToggleModule POST /api/modules/{type}/{id}/toggle
func (s *Server) handleToggleModule(w http.ResponseWriter, r *http.Request) {
	typ := ModuleType(chi.URLParam(r, "type"))
	id := chi.URLParam(r, "id")

	entry := GetModuleRegistry().Get(typ, id)
	if entry == nil {
		http.Error(w, `{"error":"模块未找到"}`, http.StatusNotFound)
		return
	}

	entry.Enabled = !entry.Enabled

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"enabled": entry.Enabled,
	})
}
