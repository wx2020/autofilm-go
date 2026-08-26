package web

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
)

// loginLimiter 内存登录限速器：按 IP+用户名统计连续失败次数，
// 达到阈值后按指数退避锁定，成功登录即重置。
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string]*loginState
}

type loginState struct {
	count int
	until time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{failures: make(map[string]*loginState)}
}

func (l *loginLimiter) key(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host + "|" + username
}

func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.failures[key]
	if !ok {
		return true
	}
	return !time.Now().Before(st.until)
}

func (l *loginLimiter) failure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.failures[key]
	if st == nil {
		st = &loginState{}
		l.failures[key] = st
	}
	st.count++
	if st.count > 20 {
		st.count = 20
	}
	if st.count < 5 {
		return
	}
	backoff := 30 * time.Second << uint(st.count-5)
	if backoff > 15*time.Minute {
		backoff = 15 * time.Minute
	}
	st.until = time.Now().Add(backoff)
}

func (l *loginLimiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

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
	var body struct{ Username, Password string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	// 事务内校验并创建首个管理员，消除并发 bootstrap 竞态
	user, err := store.BootstrapFirstUser(body.Username, body.Password)
	if err != nil {
		if err.Error() == "系统已经初始化" {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
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
	if store == nil || json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body) != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	// 登录限速：连续失败后按指数退避锁定，缓解暴力破解
	key := s.logins.key(r, body.Username)
	if !s.logins.allow(key) {
		writeJSONError(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}
	user, err := store.Authenticate(body.Username, body.Password)
	if err != nil {
		s.logins.failure(key)
		writeJSONError(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.logins.success(key)
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
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body) != nil {
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
	if id < 1 || json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body) != nil {
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
