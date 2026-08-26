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

// handleAlistTest POST /api/alist/test
// 测试 Alist 连通性：创建临时客户端（不进入全局缓存）并调用 /api/me。
// 仅接受 POST JSON，避免凭据出现在 URL query 中被记录。
func (s *Server) handleAlistTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "请使用 POST 请求")
		return
	}
	var body struct {
		URL, Username, Password, Token string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	url := body.URL

	if url == "" {
		http.Error(w, `{"error":"缺少 url 参数"}`, http.StatusBadRequest)
		return
	}

	client, err := alist.NewStandalone(url, body.Username, body.Password, body.Token)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// 尝试 fs/list 一次根目录
	_, err = client.FSListLight(r.Context(), "/")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Alist 连接正常",
	})
}
