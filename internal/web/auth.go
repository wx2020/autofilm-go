package web

import (
	"net/http"
)

// tokenAuth 简单的 Token 鉴权中间件
// 从配置文件读取 Settings.Web.Token，若为空则放行所有请求（仅本机访问）
func (s *Server) tokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := s.webConfig.Token
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// 优先从 Authorization header 读取
		auth := r.Header.Get("Authorization")
		if auth == "Bearer "+token || auth == "Token "+token {
			next.ServeHTTP(w, r)
			return
		}

		// 降级到查询参数（方便 WS 和 SSE）
		if r.URL.Query().Get("token") == token {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, `{"error":"未授权"}`, http.StatusUnauthorized)
	})
}
