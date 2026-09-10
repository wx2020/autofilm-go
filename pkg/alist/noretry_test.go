package alist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
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

// TestFSMoveUsesLongTimeout 写操作走超长通道：服务端 12s 才回包也应成功（读通道 10s 会掐线）
func TestFSMoveUsesLongTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"success","data":{"base_path":"/","id":1}}`))
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"success","data":{"token":"tk"}}`))
		case "/api/fs/move":
			time.Sleep(12 * time.Second)
			w.Write([]byte(`{"code":200,"message":"success","data":{}}`))
		default:
			w.Write([]byte(`{"code":200,"message":"success","data":{}}`))
		}
	}))
	defer srv.Close()

	client, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	start := time.Now()
	if err := client.FSMove(context.Background(), "/src", "/dst", []string{"a.mkv"}); err != nil {
		t.Fatalf("FSMove 应在超长通道内成功: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 12*time.Second {
		t.Fatalf("耗时 %v，未走超长等待（<12s 说明被提前掐断或未真实等待）", elapsed)
	}
}
