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
	errors := make(chan error, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if j%2 == 0 {
					SetGlobalStore(&Store{})
				} else {
					store := GlobalStore()
					if store == nil {
						errors <- nil
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err == nil {
			t.Error("GlobalStore 返回 nil，存在竞态条件")
			break
		}
	}
}

func TestConcurrentSetGlobalStore(t *testing.T) {
	originalStore := globalStore
	defer func() {
		globalStore = originalStore
	}()

	var wg sync.WaitGroup
	done := make(chan bool, 100)

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
			done <- true
		}(i)
	}

	wg.Wait()
	close(done)

	for d := range done {
		if !d {
			t.Error("收到错误信号")
		}
	}
}
