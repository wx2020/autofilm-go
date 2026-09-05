package web

import (
	"sync"
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
