package web

import (
	"encoding/json"
	"net/http"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
)

// handleListDbConfigs GET /api/configs/{type}  列出 DB 中的模块配置
func (s *Server) handleListDbConfigs(w http.ResponseWriter, r *http.Request) {
	typ := chi.URLParam(r, "type")
	store := storage.GlobalStore()
	if store == nil {
		http.Error(w, `{"error":"数据库不可用"}`, http.StatusServiceUnavailable)
		return
	}
	var list []map[string]interface{}
	var err error
	switch typ {
	case "alist2strm":
		list, err = store.ListAlist2StrmConfigs()
	case "ani2alist":
		list, err = store.ListAni2AlistConfigs()
	case "alissync":
		list, err = store.ListAlisyncConfigs()
	default:
		http.Error(w, `{"error":"未知类型"}`, http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"查询失败"}`, http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	json.NewEncoder(w).Encode(list)
}

// handleSaveDbConfig POST /api/configs/{type}  创建或更新模块配置
func (s *Server) handleSaveDbConfig(w http.ResponseWriter, r *http.Request) {
	typ := chi.URLParam(r, "type")
	store := storage.GlobalStore()
	if store == nil {
		http.Error(w, `{"error":"数据库不可用"}`, http.StatusServiceUnavailable)
		return
	}
	var cfg map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, `{"error":"解析失败"}`, http.StatusBadRequest)
		return
	}
	if err := store.ImportConfigMap(typ, cfg); err != nil {
		http.Error(w, `{"error":"保存失败"}`, http.StatusInternalServerError)
		return
	}
	core.TriggerReload()
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// handleDeleteDbConfig DELETE /api/configs/{type}/{id}  删除模块配置
func (s *Server) handleDeleteDbConfig(w http.ResponseWriter, r *http.Request) {
	typ := chi.URLParam(r, "type")
	id := chi.URLParam(r, "id")
	store := storage.GlobalStore()
	if store == nil {
		http.Error(w, `{"error":"数据库不可用"}`, http.StatusServiceUnavailable)
		return
	}
	var table string
	switch typ {
	case "alist2strm":
		table = "alist2strm_configs"
	case "ani2alist":
		table = "ani2alist_configs"
	case "alissync":
		table = "alisync_configs"
	default:
		http.Error(w, `{"error":"未知类型"}`, http.StatusBadRequest)
		return
	}
	_, err := store.DB().Exec("DELETE FROM "+table+" WHERE cfg_id = ?", id)
	if err != nil {
		http.Error(w, `{"error":"删除失败"}`, http.StatusInternalServerError)
		return
	}
	core.TriggerReload()
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}
