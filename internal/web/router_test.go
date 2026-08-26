package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedWebUI(t *testing.T) {
	handler := NewServer(&WebConfig{Enabled: true, Host: "127.0.0.1", Port: 8080}).httpServer.Handler

	for _, path := range []string{"/", "/alist2strm"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `<div id="app"></div>`) {
			t.Fatalf("GET %s did not return the SPA entry point", path)
		}
	}
}

func TestMissingAPIIsNotSPA(t *testing.T) {
	handler := NewServer(&WebConfig{Enabled: true, Host: "127.0.0.1", Port: 8080}).httpServer.Handler
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/not-found", nil))
	// 未认证请求在进入路由前被 401 拦截；认证后的未知接口仍为 404。
	// 两者都不应返回 SPA 入口页。
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 404 or 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `<div id="app"></div>`) {
		t.Fatal("missing API returned the SPA entry point")
	}
}

func TestHealthRemainsAvailableWithToken(t *testing.T) {
	handler := NewServer(&WebConfig{Enabled: true, Host: "127.0.0.1", Port: 8080, Token: "secret"}).httpServer.Handler
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
