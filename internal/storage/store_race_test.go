package storage

import (
	"sync"
	"testing"
	"time"
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

	done := make(chan bool)

	for i := 0; i < 100; i++ {
		go func(id int) {
			s := &Store{}
			SetGlobalStore(s)
			time.Sleep(time.Microsecond)
			got := GlobalStore()
			if got != s {
				t.Errorf("goroutine %d: 期望获取到设置的 Store，实际获取到 %v", id, got)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}
