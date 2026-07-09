package web

import (
	"net/http"

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
	r.Use(s.tokenAuth)

	// API 路由
	r.Route("/api", func(r chi.Router) {
		// 健康检查
		r.Get("/health", s.handleHealth)

		// Alist 连通性测试
		r.Get("/alist/test", s.handleAlistTest)

		// 配置
		r.Get("/config", s.handleGetConfig)
		r.Put("/config", s.handleUpdateConfig)

		// 模块
		r.Get("/modules", s.handleListModules)
		r.Post("/modules/{type}/{id}/run", s.handleRunModule)
		r.Post("/modules/{type}/{id}/toggle", s.handleToggleModule)

		// 同步队列
		r.Get("/sync/queue", s.handleGetSyncQueue)
		r.Post("/sync/queue/retry/{tid}", s.handleRetrySyncTask)

		// 日志
		r.Get("/logs", s.handleGetLogs)
	})

	// WebSocket 日志流（独立路由，避免被中间件干扰查询参数）
	r.Get("/api/logs/stream", s.handleLogStream)

	// 前端静态文件
	fileServer := http.FileServer(http.Dir("internal/web/dist"))
	r.Handle("/*", fileServer)

	return r
}
