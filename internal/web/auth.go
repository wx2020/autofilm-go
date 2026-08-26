package web

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/akimio/autofilm/internal/storage"
)

type authUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type authContextKey struct{}

// bearerToken 提取访问令牌。
// WebSocket 握手无法携带自定义 Header，仅对升级请求回退读取 query 参数；
// 普通请求不再接受 query 传参，避免凭据进入访问日志。
func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	for _, prefix := range []string{"Bearer ", "Token "} {
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		}
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return r.URL.Query().Get("token")
	}
	return ""
}

func (s *Server) tokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)

		// A configured web token is an explicit administrator access token.
		// It remains valid after the first database user is created.
		if s.webConfig.Token != "" && token != "" &&
			subtle.ConstantTimeCompare([]byte(token), []byte(s.webConfig.Token)) == 1 {
			ctx := context.WithValue(r.Context(), authContextKey{}, authUser{Username: "legacy-admin", Role: "admin"})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// 数据库会话认证。首次启动前必须通过 /api/auth/bootstrap 创建管理员，
		// 未初始化状态下受保护 API 一律返回 401，消除零鉴权窗口。
		if store := storage.GlobalStore(); store != nil && token != "" {
			if user, err := store.UserBySession(token); err == nil {
				ctx := context.WithValue(r.Context(), authContextKey{}, authUser{ID: user.ID, Username: user.Username, Role: user.Role})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		writeJSONError(w, http.StatusUnauthorized, "未授权或登录已失效")
	})
}

func currentUser(r *http.Request) authUser {
	user, _ := r.Context().Value(authContextKey{}).(authUser)
	return user
}

func requireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allowed[currentUser(r).Role] {
				writeJSONError(w, http.StatusForbidden, "权限不足")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
