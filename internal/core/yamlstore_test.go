package core

import (
	"sync"
	"testing"
	"time"
)

// TestYamlStoreSubscribe_NotifyOnReload 验证 Subscribe 在 Reload 时收到通知
func TestYamlStoreSubscribe_NotifyOnReload(t *testing.T) {
	store := NewYamlStore()

	ch, unsub := store.Subscribe()
	defer unsub()

	if err := store.Reload(); err != nil {
		t.Fatalf("Reload 失败: %v", err)
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("超时：未收到 Reload 通知")
	}
}

// TestYamlStoreSubscribe_MultipleSubscribers 验证多个订阅者都能收到通知
func TestYamlStoreSubscribe_MultipleSubscribers(t *testing.T) {
	store := NewYamlStore()

	ch1, u1 := store.Subscribe()
	defer u1()
	ch2, u2 := store.Subscribe()
	defer u2()

	if err := store.Reload(); err != nil {
		t.Fatalf("Reload 失败: %v", err)
	}

	received := 0
	deadline := time.After(time.Second)
	for received < 2 {
		select {
		case <-ch1:
			received++
		case <-ch2:
			received++
		case <-deadline:
			t.Fatalf("超时：仅收到 %d/2 个通知", received)
		}
	}
}

// TestYamlStoreSubscribe_UnsubClosesChannel 验证 unsub 关闭 channel
func TestYamlStoreSubscribe_UnsubClosesChannel(t *testing.T) {
	store := NewYamlStore()

	ch, unsub := store.Subscribe()
	unsub()

	// channel 必须已关闭：读两次均 ok=false
	if _, ok := <-ch; ok {
		t.Fatal("unsub 后 channel 未关闭（首次读返回 ok=true）")
	}
	if _, ok := <-ch; ok {
		t.Fatal("unsub 后 channel 未关闭（第二次读返回 ok=true）")
	}
}

// TestYamlStoreSubscribe_NonBlockingNotify 验证 notify 非阻塞：慢订阅者不会拖住通知
func TestYamlStoreSubscribe_NonBlockingNotify(t *testing.T) {
	store := NewYamlStore()

	ch, unsub := store.Subscribe()
	defer unsub()

	// 第一次 Reload 填满 buffer
	if err := store.Reload(); err != nil {
		t.Fatalf("首次 Reload: %v", err)
	}
	// 第二次 Reload 应在合理时间内返回（不被订阅者阻塞）
	done := make(chan struct{})
	go func() {
		_ = store.Reload()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Reload 被慢订阅者阻塞")
	}

	// 至少能消费到一次通知（可能因合并而少于两次）
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("未收到任何通知")
	}
}

// TestYamlStoreGetMethods_NoPanic 验证各 Get 方法不 panic
func TestYamlStoreGetMethods_NoPanic(t *testing.T) {
	store := NewYamlStore()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Get 方法 panic: %v", r)
		}
	}()
	_ = store.IsDebug()
	_ = store.GetTimezone()
	_ = store.GetConfigDir()
	_ = store.GetLogDir()
	_ = store.GetConfigFile()
	_ = store.GetAlist2StrmList()
	_ = store.GetAni2AlistList()
	_ = store.GetLibraryPosterList()
	_ = store.GetAlistSyncList()
}

// TestYamlStoreImplementsConfigStore 编译期保证 YamlStore 满足 ConfigStore 接口
func TestYamlStoreImplementsConfigStore(t *testing.T) {
	var _ ConfigStore = (*YamlStore)(nil)
}

// TestYamlStoreInit_Ok 验证 Init 不报错
func TestYamlStoreInit_Ok(t *testing.T) {
	store := NewYamlStore()
	if err := store.Init(); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
}

// TestYamlStoreSubscribe_ConcurrentSafe 并发 Subscribe/Unsubscribe/Reload 不应触发竞态
// （在 -race 模式下可暴露数据竞争）
func TestYamlStoreSubscribe_ConcurrentSafe(t *testing.T) {
	store := NewYamlStore()
	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, unsub := store.Subscribe()
			defer unsub()
			_ = store.Reload()
			select {
			case <-ch:
			case <-time.After(500 * time.Millisecond):
			}
		}()
	}
	wg.Wait()
}
