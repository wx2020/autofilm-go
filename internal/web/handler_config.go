package web

import (
	"encoding/json"
	"net/http"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/storage"
)

func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	store := storage.GlobalStore()
	if store == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	settings, err := store.GetAppSettings()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	store := storage.GlobalStore()
	if store == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	var settings storage.AppSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&settings); err != nil {
		writeJSONError(w, http.StatusBadRequest, "设置格式无效")
		return
	}
	if err := store.SaveAppSettings(settings); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	core.TriggerReload()
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "restart_required": true})
}
