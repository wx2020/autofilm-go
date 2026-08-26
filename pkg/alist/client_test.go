package alist

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestGetTokenPasswordAuthNoDeadlock 回归测试：
// 用户名/密码认证（tokenExp 初始为 0）首次调用必须能完成登录，
// 不得因写锁内再次 RLock 造成自锁死锁。
func TestGetTokenPasswordAuthNoDeadlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"new-token"}}`))
		case "/api/me":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := NewStandalone(srv.URL, "user", "pass", "")
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	done := make(chan string, 1)
	go func() {
		done <- client.getToken()
	}()
	select {
	case tok := <-done:
		if tok != "new-token" {
			t.Fatalf("token = %q, want new-token", tok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("getToken 死锁：5 秒内未返回")
	}
}

func TestSuffixLowercase(t *testing.T) {
	p := &AlistPath{Name: "MOVIE.MKV"}
	if got := p.Suffix(); got != ".mkv" {
		t.Fatalf("Suffix() = %q, want .mkv", got)
	}
	p = &AlistPath{Name: "Sub.SRT"}
	if got := p.Suffix(); got != ".srt" {
		t.Fatalf("Suffix() = %q, want .srt", got)
	}
	p = &AlistPath{Name: "noext"}
	if got := p.Suffix(); got != "" {
		t.Fatalf("Suffix() = %q, want empty", got)
	}
}

func TestClientCacheKeyDistinguishesCredentials(t *testing.T) {
	_, k1 := clientCacheKey("http://a.com", "u", "p1", "t")
	_, k2 := clientCacheKey("http://a.com", "u", "p2", "t")
	_, k3 := clientCacheKey("http://a.com", "u", "p1", "t")
	if k1 == k2 {
		t.Fatal("不同密码的缓存键不应相同")
	}
	if k1 != k3 {
		t.Fatal("相同凭据的缓存键应一致")
	}
}
