package storage

import (
	"sync"
	"testing"
)

func TestGlobalStoreRace(t *testing.T) {
	originalStore := globalStore
	defer func() {
		globalStore = originalStore
	}()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if j%2 == 0 {
					SetGlobalStore(&Store{})
				} else {
					GlobalStore()
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestConcurrentSetGlobalStore(t *testing.T) {
	originalStore := globalStore
	defer func() {
		globalStore = originalStore
	}()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s := &Store{}
			SetGlobalStore(s)
			got := GlobalStore()
			if got != s {
				t.Errorf("goroutine %d: 期望获取到设置的 Store，实际获取到 %v", id, got)
			}
		}(i)
	}
	wg.Wait()
}
