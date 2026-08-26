package alist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newCountingServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"ok","data":{"token":"tk"}}`))
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"ok","data":{"base_path":"/","id":1}}`))
		default:
			w.Write([]byte(`{"code":200,"message":"ok","data":{}}`))
		}
		count.Add(1)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

// TestRateLimitThrottlesRequests 实测令牌桶真实生效：
// qps=5、burst=5 时，20 个并发请求应被拉长到约 3 秒完成。
func TestRateLimitThrottlesRequests(t *testing.T) {
	srv, count := newCountingServer(t)
	client, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	client.SetRateLimit(5)

	// NewStandalone 初始化（/api/me + 登录）也会计入总数，先取基线
	baseline := count.Load()

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.doRequest(context.Background(), "GET", "/api/me", nil); err != nil {
				t.Errorf("doRequest: %v", err)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if got := count.Load() - baseline; got != 20 {
		t.Fatalf("请求数 = %d, want 20", got)
	}
	// 理论下界：(20-burst)/qps = 15/5 = 3s。只断言下界，避免调度抖动导致误报；
	// 若限流失效，全部请求会在毫秒级完成，2s 下界必然失败。
	if elapsed < 2*time.Second {
		t.Fatalf("限流未生效：20 个请求在 %v 内完成（应 ≥2s）", elapsed)
	}
}

// TestSetRateLimitConcurrentWithRequests 在 -race 下验证运行中改写限流器无数据竞争
func TestSetRateLimitConcurrentWithRequests(t *testing.T) {
	srv, _ := newCountingServer(t)
	client, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				client.SetRateLimit(10)
			} else {
				client.SetRateLimit(1)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, _ = client.doRequest(context.Background(), "GET", "/api/me", nil)
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	client.SetRateLimit(0) // 取消限流，让剩余请求快速结束
	wg.Wait()
}

// TestLimitQPSAccessor 验证 LimitQPS 反映当前限流策略（0 表示不限流）
func TestLimitQPSAccessor(t *testing.T) {
	srv, _ := newCountingServer(t)
	client, err := NewStandalone(srv.URL, "u", "p", "")
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	if got := client.LimitQPS(); got != 0 {
		t.Fatalf("初始 LimitQPS() = %d, want 0", got)
	}
	client.SetRateLimit(7)
	if got := client.LimitQPS(); got != 7 {
		t.Fatalf("设置后 LimitQPS() = %d, want 7", got)
	}
	client.SetRateLimit(0)
	if got := client.LimitQPS(); got != 0 {
		t.Fatalf("清除后 LimitQPS() = %d, want 0", got)
	}
}
