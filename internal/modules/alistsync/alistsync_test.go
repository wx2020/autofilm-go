package alistsync

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
	"github.com/sirupsen/logrus"
)

// TestNewAppliesQPSLimit 验证 alistsync 的 qps_limit 配置真实应用到共享客户端
func TestNewAppliesQPSLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"ok","data":{"token":"tk"}}`))
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"ok","data":{"base_path":"/","id":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		ID:       "sync-a",
		URL:      srv.URL,
		Username: "user",
		Password: "pass",
		Retry:    RetryConfig{MaxAttempts: 10, Backoff: "expo"},
		QPSLimit: 7,
	}
	if _, err := New(cfg); err != nil {
		t.Fatalf("New: %v", err)
	}

	// GetClient 返回的是与 New 共享的缓存实例，应能观察到限流策略
	shared, err := alist.GetClient(srv.URL, "user", "pass", "")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if got := shared.LimitQPS(); got != 7 {
		t.Fatalf("LimitQPS() = %d, want 7", got)
	}
}

func TestShouldOverwrite(t *testing.T) {
	src := &alist.AlistPath{Modified: "2026-07-28T10:00:00Z"}
	old := &alist.AlistPath{Modified: "2026-07-27T10:00:00Z"}
	newer := &alist.AlistPath{Modified: "2026-07-29T10:00:00Z"}
	if !ShouldOverwrite(OverwriteAlways, src, newer) {
		t.Fatal("always rejected")
	}
	if ShouldOverwrite(OverwriteNever, src, nil) {
		t.Fatal("never accepted")
	}
	if !ShouldOverwrite(OverwriteIfNewer, src, old) {
		t.Fatal("newer source rejected")
	}
	if ShouldOverwrite(OverwriteIfNewer, src, newer) {
		t.Fatal("older source accepted")
	}
}

func TestRetryBackoffGrows(t *testing.T) {
	rand.Seed(1)
	d := NewRetryDaemon(nil, nil, &RetryConfig{MaxAttempts: 10, Backoff: "expo", Jitter: 0}, logrus.New())
	now := time.Now()
	first := d.calcNextRetry(1).Sub(now)
	second := d.calcNextRetry(2).Sub(now)
	if first < 25*time.Second || second <= first {
		t.Fatalf("backoff did not grow: %v %v", first, second)
	}
}
