package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleAuthStatus(w http.ResponseWriter, _ *http.Request) {
	count := 0
	if store := storage.GlobalStore(); store != nil {
		count, _ = store.UserCount()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"initialized": count > 0, "legacy_token": s.webConfig.Token != ""})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	store := storage.GlobalStore()
	if store == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	count, err := store.UserCount()
	if err != nil || count != 0 {
		writeJSONError(w, http.StatusConflict, "系统已经初始化")
		return
	}
	var body struct{ Username, Password string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	user, err := store.CreateUser(body.Username, body.Password, "admin")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, err := store.CreateSession(user.ID, 24*time.Hour)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"token": token, "user": user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	store := storage.GlobalStore()
	var body struct{ Username, Password string }
	if store == nil || json.NewDecoder(r.Body).Decode(&body) != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	user, err := store.Authenticate(body.Username, body.Password)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	token, err := store.CreateSession(user.ID, 24*time.Hour)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"token": token, "user": user})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if store := storage.GlobalStore(); store != nil {
		_ = store.DeleteSession(bearerToken(r))
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := storage.GlobalStore().ListUsers()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password, Role string }
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	user, err := storage.GlobalStore().CreateUser(body.Username, body.Password, body.Role)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var body struct {
		Role     string `json:"role"`
		Enabled  bool   `json:"enabled"`
		Password string `json:"password"`
	}
	if id < 1 || json.NewDecoder(r.Body).Decode(&body) != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if err := storage.GlobalStore().UpdateUser(id, body.Role, body.Enabled, body.Password); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id == currentUser(r).ID {
		writeJSONError(w, http.StatusBadRequest, "不能删除当前用户")
		return
	}
	if err := storage.GlobalStore().DeleteUser(id); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleBackup(w http.ResponseWriter, _ *http.Request) {
	backup, err := storage.GlobalStore().ExportBackup()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="autofilm-backup-%s.json"`, time.Now().Format("20060102-150405")))
	writeJSON(w, http.StatusOK, backup)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	var backup storage.Backup
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 10<<20)).Decode(&backup); err != nil {
		writeJSONError(w, http.StatusBadRequest, "备份文件无效")
		return
	}
	if err := storage.GlobalStore().ImportBackup(&backup); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	core.TriggerReload()
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleAlerts(w http.ResponseWriter, _ *http.Request) {
	alerts, err := storage.GlobalStore().ListAlerts(200)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (s *Server) handleAcknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := storage.GlobalStore().AcknowledgeAlert(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
