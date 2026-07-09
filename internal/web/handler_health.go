package web

import (
	"encoding/json"
	"net/http"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/pkg/alist"
)

// handleHealth GET /api/health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"version": core.AppVersion(),
	})
}

// handleAlistTest GET /api/alist/test
// 测试 Alist 连通性：创建临时客户端并调用 /api/me
func (s *Server) handleAlistTest(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	username := r.URL.Query().Get("username")
	password := r.URL.Query().Get("password")
	token := r.URL.Query().Get("token")

	if url == "" {
		http.Error(w, `{"error":"缺少 url 参数"}`, http.StatusBadRequest)
		return
	}

	client, err := alist.GetClient(url, username, password, token)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// 尝试 fs/list 一次根目录
	_, err = client.FSListLight(r.Context(), "/")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Alist 连接正常",
	})
}
