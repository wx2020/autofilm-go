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

// TestManualRunDoesNotTriggerOtherTasks 手动触发任务 A 时，任务 B 的执行函数不得被调用
func TestManualRunDoesNotTriggerOtherTasks(t *testing.T) {
	typ := ModuleType("test-isolation")
	var mu sync.Mutex
	ran := map[string]int{}
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once

	makeFunc := func(id string) func() {
		return func() {
			mu.Lock()
			ran[id]++
			mu.Unlock()
			if id == "task-a" {
				once.Do(func() { close(started) })
				<-release // 保持 A 占用执行锁，直到测试放行
			}
		}
	}

	for _, id := range []string{"task-a", "task-b"} {
		GetModuleRegistry().Register(&ModuleEntry{
			Type: typ, ID: id, Enabled: true, Cron: "0 0 * * * *",
			RunFunc: makeFunc(id),
		})
		defer GetModuleRegistry().Unregister(typ, id)
		defer GetModuleRegistry().ReleaseRun(typ, id)
	}

	srv := NewServer(&WebConfig{Enabled: true})

	// 手动触发 A 成功，A 开始执行并持有锁
	if rec := runModuleRequest(srv, typ, "task-a"); rec.Code != http.StatusOK {
		t.Fatalf("触发 A status = %d, want 200", rec.Code)
	}
	<-started

	// 运行中重复触发 A 应被拒绝
	if rec := runModuleRequest(srv, typ, "task-a"); rec.Code != http.StatusConflict {
		t.Fatalf("运行中重复触发 A status = %d, want 409", rec.Code)
	}

	// 放行 A 并等待锁释放
	close(release)
	released := false
	for i := 0; i < 500; i++ {
		if !GetModuleRegistry().IsRunning(typ, "task-a") {
			released = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !released {
		t.Fatal("任务 A 结束后锁未释放")
	}

	mu.Lock()
	defer mu.Unlock()
	if ran["task-a"] != 1 {
		t.Fatalf("任务 A 执行次数 = %d, want 1", ran["task-a"])
	}
	if ran["task-b"] != 0 {
		t.Fatalf("任务 B 被意外执行 %d 次", ran["task-b"])
	}
}
