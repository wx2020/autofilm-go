package web

import (
	"net/http"
	"os"
	"path/filepath"
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

// WebRoot 前端静态文件目录（可由 main 注入）
var WebRoot = "internal/web/dist"

// newRouter 创建路由
func (s *Server) newRouter() http.Handler {
	r := chi.NewRouter()

	// 中间件
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(s.tokenAuth)

	// API 路由
	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Get("/alist/test", s.handleAlistTest)
		r.Get("/config", s.handleGetConfig)
		r.Put("/config", s.handleUpdateConfig)
		r.Get("/modules", s.handleListModules)
		r.Post("/modules/{type}/{id}/run", s.handleRunModule)
		r.Post("/modules/{type}/{id}/toggle", s.handleToggleModule)
		r.Get("/sync/queue", s.handleGetSyncQueue)
		r.Post("/sync/queue/retry/{tid}", s.handleRetrySyncTask)
		r.Get("/logs", s.handleGetLogs)
	})

	// DB 配置 CRUD
	r.Get("/configs/{type}", s.handleListDbConfigs)
	r.Post("/configs/{type}", s.handleSaveDbConfig)
	r.Delete("/configs/{type}/{id}", s.handleDeleteDbConfig)

	// WebSocket 日志流
	r.Get("/api/logs/stream", s.handleLogStream)

	// 前端 SPA：先尝试匹配静态文件，匹配不到则返回 index.html
	fileServer := http.FileServer(http.Dir(WebRoot))
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		fullPath := filepath.Join(WebRoot, path)
		// 文件存在则直接提供
		if fi, err := os.Stat(fullPath); err == nil && !fi.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// 否则返回 index.html（SPA fallback）
		r.URL.Path = "/index.html"
		fileServer.ServeHTTP(w, r)
	})

	return r
}
