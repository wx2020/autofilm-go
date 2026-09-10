package alist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// eofGetServer /api/fs/get 恒返回驱动 EOF，并计数命中次数
func eofGetServer(t *testing.T, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		case "/api/fs/get":
			hits.Add(1)
			w.Write([]byte(`{"code":500,"message":"EOF","data":null}`))
		default:
			w.Write([]byte(`{"code":200,"message":"success","data":{}}`))
		}
	}))
}

// TestFSGetNoRetrySkipsBackoff P1：探测类 FSGet 只打一次，不触发 1s+2s 退避重试
func TestFSGetNoRetrySkipsBackoff(t *testing.T) {
	var hits atomic.Int64
	srv := eofGetServer(t, &hits)
	defer srv.Close()

	client, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	if _, err := client.FSGetNoRetry(context.Background(), "/x/a.mkv"); err == nil {
		t.Fatal("应返回错误")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("FSGetNoRetry 命中 /api/fs/get = %d 次，want 1（免重试）", got)
	}
}
