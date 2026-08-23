package web

import (
	"context"
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

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	for _, prefix := range []string{"Bearer ", "Token "} {
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		}
	}
	return r.URL.Query().Get("token")
}

func (s *Server) tokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		store := storage.GlobalStore()
		// A configured web token is an explicit administrator access token.
		// It remains valid after the first database user is created.
		if s.webConfig.Token != "" && token == s.webConfig.Token {
			ctx := context.WithValue(r.Context(), authContextKey{}, authUser{Username: "legacy-admin", Role: "admin"})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if store != nil {
			if count, err := store.UserCount(); err == nil && count > 0 {
				user, err := store.UserBySession(token)
				if err != nil {
					writeJSONError(w, http.StatusUnauthorized, "登录已失效，请重新登录")
					return
				}
				ctx := context.WithValue(r.Context(), authContextKey{}, authUser{ID: user.ID, Username: user.Username, Role: user.Role})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 首次启动前兼容单 Token；创建首个管理员后自动切换到用户会话。
		if s.webConfig.Token == "" {
			ctx := context.WithValue(r.Context(), authContextKey{}, authUser{Username: "legacy-admin", Role: "admin"})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "未授权")
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
