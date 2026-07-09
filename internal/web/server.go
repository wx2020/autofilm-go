package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/sirupsen/logrus"
)

// Server Web 服务器
type Server struct {
	httpServer *http.Server
	webConfig  *WebConfig
	logger     *logrus.Logger
}

// NewServer 创建 Web 服务器
func NewServer(cfg *WebConfig) *Server {
	s := &Server{
		webConfig: cfg,
		logger:    core.GetLogger(),
	}
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:      s.newRouter(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return s
}

// Start 启动 HTTP 服务（非阻塞）
func (s *Server) Start() error {
	if !s.webConfig.Enabled {
		s.logger.Info("Web 服务未启用")
		return nil
	}

	s.logger.Infof("Web 服务启动于 http://%s:%d", s.webConfig.Host, s.webConfig.Port)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("Web 服务错误: %v", err)
		}
	}()

	return nil
}

// Shutdown 优雅关闭
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}
