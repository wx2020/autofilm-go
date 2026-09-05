package web

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNextRunAcrossCronFormats 验证共享解析器一致性：
// 6 段、5 段、描述符表达式都能算出有效的下次运行时间（不再显示 1/1/1），
// 非法表达式与禁用模块返回零时间（前端负责隐藏）。
func TestNextRunAcrossCronFormats(t *testing.T) {
	r := NewModuleRegistry()
	valid := map[string]string{
		"six":   "0 0 */6 * * *",
		"five":  "0 */6 * * *",
		"daily": "@daily",
		"every": "@every 10m",
	}
	for id, spec := range valid {
		r.Register(&ModuleEntry{Type: ModuleAlist2Strm, ID: id, Enabled: true, Cron: spec})
		got := r.Get(ModuleAlist2Strm, id)
		if got.NextRun.IsZero() {
			t.Errorf("cron %q 解析结果为零时间（会显示 1/1/1）", spec)
			continue
		}
		if got.NextRun.Before(time.Now()) {
			t.Errorf("cron %q 的下次运行时间 %v 在过去", spec, got.NextRun)
		}
	}
	// 同一份解析器必须同时被调度器与校验使用
	for spec := range valid {
		if _, err := SharedCronParser().Parse(valid[spec]); err != nil {
			t.Errorf("共享解析器无法解析 %q: %v", valid[spec], err)
		}
	}

	r.Register(&ModuleEntry{Type: ModuleAlist2Strm, ID: "bad", Enabled: true, Cron: "not a cron"})
	if !r.Get(ModuleAlist2Strm, "bad").NextRun.IsZero() {
		t.Error("非法表达式应返回零时间")
	}
	r.Register(&ModuleEntry{Type: ModuleAlist2Strm, ID: "off", Enabled: false, Cron: "0 0 */6 * * *"})
	if !r.Get(ModuleAlist2Strm, "off").NextRun.IsZero() {
		t.Error("禁用模块应返回零时间")
	}
}

// TestRegistryConcurrentAccess 在 -race 下验证并发 List/Get/SetEnabled 安全
func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewModuleRegistry()
	r.Register(&ModuleEntry{Type: ModuleAlist2Strm, ID: "a", Enabled: true, Cron: "0 0 * * * *"})
	r.Register(&ModuleEntry{Type: ModuleFileMove, ID: "b", Enabled: false, Cron: ""})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = r.List()
				_ = r.ListByType(ModuleAlist2Strm)
				e := r.Get(ModuleAlist2Strm, "a")
				if e == nil || e.ID != "a" {
					t.Error("Get returned unexpected entry")
					return
				}
				if !r.IsEnabled(ModuleFileMove, "b") && i%2 == 0 {
					r.SetEnabled(ModuleFileMove, "b", true)
				} else if r.IsEnabled(ModuleFileMove, "b") && i%2 == 1 {
					r.SetEnabled(ModuleFileMove, "b", false)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestRegistryRegisterDoesNotAliasEntry(t *testing.T) {
	r := NewModuleRegistry()
	entry := &ModuleEntry{Type: ModuleAni2Alist, ID: "x", Enabled: true, Cron: "5 5 * * * *"}
	r.Register(entry)
	stored := r.Get(ModuleAni2Alist, "x")
	stored.Enabled = false

	if !r.IsEnabled(ModuleAni2Alist, "x") {
		t.Fatal("修改 Get 返回的副本不应影响注册表内的状态")
	}
}

// TestRunLockSingleFlight 同一任务同时只允许抢占一次执行权
func TestRunLockSingleFlight(t *testing.T) {
	r := NewModuleRegistry()

	if !r.TryAcquireRun(ModuleAlist2Strm, "job-1") {
		t.Fatal("首次抢占应成功")
	}
	if !r.IsRunning(ModuleAlist2Strm, "job-1") {
		t.Fatal("抢占后 IsRunning 应为 true")
	}
	if r.TryAcquireRun(ModuleAlist2Strm, "job-1") {
		t.Fatal("运行中重复抢占应失败")
	}
	// 不同任务互不影响
	if !r.TryAcquireRun(ModuleAlist2Strm, "job-2") {
		t.Fatal("不同任务的抢占应成功")
	}
	r.ReleaseRun(ModuleAlist2Strm, "job-1")
	if r.IsRunning(ModuleAlist2Strm, "job-1") {
		t.Fatal("释放后 IsRunning 应为 false")
	}
	if !r.TryAcquireRun(ModuleAlist2Strm, "job-1") {
		t.Fatal("释放后再次抢占应成功")
	}
	r.ReleaseRun(ModuleAlist2Strm, "job-1")
	r.ReleaseRun(ModuleAlist2Strm, "job-2")

	// List 返回的副本应携带 Running 状态
	r.TryAcquireRun(ModuleFileMove, "m-1")
	r.Register(&ModuleEntry{Type: ModuleFileMove, ID: "m-1", Enabled: true})
	found := false
	for _, e := range r.List() {
		if e.Type == ModuleFileMove && e.ID == "m-1" {
			found = true
			if !e.Running {
				t.Fatal("List 副本应携带 Running=true")
			}
		}
	}
	if !found {
		t.Fatal("List 未返回已注册模块")
	}
	r.ReleaseRun(ModuleFileMove, "m-1")
}

// TestRunLockConcurrentHammer 高并发下同时最多只有一个持有者（-race 验证无竞争）
func TestRunLockConcurrentHammer(t *testing.T) {
	r := NewModuleRegistry()
	var current, maxSeen atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.TryAcquireRun(ModuleAlistSync, "hammer") {
				cur := current.Add(1)
				for {
					old := maxSeen.Load()
					if cur <= old || maxSeen.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond) // 放大重叠窗口：锁若失效必被发现
				current.Add(-1)
				r.ReleaseRun(ModuleAlistSync, "hammer")
			}
		}()
	}
	wg.Wait()
	if got := maxSeen.Load(); got != 1 {
		t.Fatalf("同时最多应只有 1 个持有者，实际最大并发 = %d", got)
	}
}
