package web

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// WebConfig Web 配置
type WebConfig struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Token   string `json:"token,omitempty"`
}

// newRouter 创建路由
func (s *Server) newRouter() http.Handler {
	r := chi.NewRouter()

	// 中间件
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Get("/api/health", s.handleHealth)
	r.Get("/metrics", handlePrometheusMetrics)
	r.Get("/api/auth/status", s.handleAuthStatus)
	r.Post("/api/auth/bootstrap", s.handleBootstrap)
	r.Post("/api/auth/login", s.handleLogin)

	// API 路由
	r.Route("/api", func(r chi.Router) {
		r.Use(s.tokenAuth)
		r.Get("/auth/me", s.handleMe)
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, metricsSnapshot()) })
		r.Get("/alist/test", s.handleAlistTest)
		r.Post("/alist/test", s.handleAlistTest)
		r.Get("/modules", s.handleListModules)
		r.Get("/sync/queue", s.handleGetSyncQueue)
		r.Get("/logs", s.handleGetLogs)
		r.Get("/logs/stream", s.handleLogStream)
		r.Get("/runs", s.handleTaskRuns)
		r.Get("/alerts", s.handleAlerts)
		r.Get("/configs/{type}", s.handleListDbConfigs)

		r.Group(func(r chi.Router) {
			r.Use(requireRole("admin", "operator"))
			r.Post("/modules/{type}/{id}/run", s.handleRunModule)
			r.Post("/modules/{type}/{id}/toggle", s.handleToggleModule)
			r.Post("/sync/queue/retry/{tid}", s.handleRetrySyncTask)
			r.Post("/alerts/{id}/ack", s.handleAcknowledgeAlert)
			r.Post("/configs/{type}", s.handleSaveDbConfig)
			r.Delete("/configs/{type}/{id}", s.handleDeleteDbConfig)
		})

		r.Group(func(r chi.Router) {
			r.Use(requireRole("admin"))
			r.Get("/config", s.handleGetConfig)
			r.Put("/config", s.handleUpdateConfig)
			r.Get("/users", s.handleUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)
			r.Get("/backup", s.handleBackup)
			r.Post("/restore", s.handleRestore)
		})
	})

	r.NotFound(spaHandler())

	return r
}

func spaHandler() http.HandlerFunc {
	distFS, err := fs.Sub(embeddedWebUI, "dist")
	if err != nil {
		panic("初始化嵌入式 WebUI 失败: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(distFS))
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "api" || strings.HasPrefix(path, "api/") {
			http.Error(w, `{"error":"接口不存在"}`, http.StatusNotFound)
			return
		}
		if path == "" {
			path = "index.html"
		}
		if info, statErr := fs.Stat(distFS, path); statErr == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(path, ".") {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}
