package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func runModuleRequest(srv *Server, typ ModuleType, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/modules/"+string(typ)+"/"+id+"/run", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("type", string(typ))
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	srv.handleRunModule(rec, req)
	return rec
}

// TestHandleRunModuleConflict 同一任务运行中再次触发应返回 409，结束后恢复可运行
func TestHandleRunModuleConflict(t *testing.T) {
	typ := ModuleType("test-single")
	id := "conflict-1"
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once

	GetModuleRegistry().Register(&ModuleEntry{
		Type: typ, ID: id, Enabled: true, Cron: "0 0 * * * *",
		RunFunc: func() {
			once.Do(func() { close(started) })
			<-release
		},
	})
	defer GetModuleRegistry().Unregister(typ, id)
	defer GetModuleRegistry().ReleaseRun(typ, id)

	srv := NewServer(&WebConfig{Enabled: true})

	// 第一次触发成功，RunFunc 开始执行并持有锁
	if rec := runModuleRequest(srv, typ, id); rec.Code != http.StatusOK {
		t.Fatalf("首次触发 status = %d, want 200", rec.Code)
	}
	<-started

	// 运行中再次触发应被拒绝
	if rec := runModuleRequest(srv, typ, id); rec.Code != http.StatusConflict {
		t.Fatalf("运行中重复触发 status = %d, want 409", rec.Code)
	}

	// 任务结束后可再次触发
	close(release)
	// 等待后台 goroutine 释放锁（轮询避免 flaky）
	deadline := make(chan struct{})
	go func() {
		defer close(deadline)
		for GetModuleRegistry().IsRunning(typ, id) {
			time.Sleep(time.Millisecond)
		}
	}()
	select {
	case <-deadline:
	case <-time.After(5 * time.Second):
		t.Fatal("任务结束后锁未释放")
	}
	if rec := runModuleRequest(srv, typ, id); rec.Code != http.StatusOK {
		t.Fatalf("结束后再次触发 status = %d, want 200", rec.Code)
	}
}
