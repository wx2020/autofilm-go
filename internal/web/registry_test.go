package web

import (
	"sync"
	"testing"
)

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
