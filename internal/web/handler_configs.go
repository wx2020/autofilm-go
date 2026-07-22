package web

import (
	"encoding/json"
	"net/http"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
)

var moduleTypes = map[string]bool{"alist2strm": true, "ani2alist": true, "libraryposter": true, "alissync": true}

func moduleStore(w http.ResponseWriter, r *http.Request) (*storage.Store, string, bool) {
	typ := chi.URLParam(r, "type")
	if !moduleTypes[typ] {
		writeJSONError(w, http.StatusBadRequest, "未知模块类型")
		return nil, "", false
	}
	store := storage.GlobalStore()
	if store == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "数据库不可用")
		return nil, "", false
	}
	return store, typ, true
}

func (s *Server) handleListDbConfigs(w http.ResponseWriter, r *http.Request) {
	store, typ, ok := moduleStore(w, r)
	if !ok {
		return
	}
	list, err := store.ListModuleConfigs(typ)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveDbConfig(w http.ResponseWriter, r *http.Request) {
	store, typ, ok := moduleStore(w, r)
	if !ok {
		return
	}
	var cfg map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, "配置格式无效")
		return
	}
	if err := store.SaveModuleConfig(typ, cfg); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	core.TriggerReload()
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleDeleteDbConfig(w http.ResponseWriter, r *http.Request) {
	store, typ, ok := moduleStore(w, r)
	if !ok {
		return
	}
	if err := store.DeleteModuleConfig(typ, chi.URLParam(r, "id")); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	core.TriggerReload()
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
