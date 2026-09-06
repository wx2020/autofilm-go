package web

import (
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestLogHookBroadcasts 验证日志钩子能把日志广播到 WebSocket 订阅者。
// 回归测试：钩子若未挂载到 logger，/api/logs/stream 将永远收不到日志。
func TestLogHookBroadcasts(t *testing.T) {
	ch := SubscribeLogs()
	defer UnsubscribeLogs(ch)

	entry := logrus.NewEntry(logrus.New())
	entry.Message = "alist2strm test line"
	entry.Level = logrus.InfoLevel
	entry.Time = time.Now()
	if err := GetLogHook().Fire(entry); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	select {
	case line := <-ch:
		if !strings.Contains(line, "alist2strm test line") {
			t.Fatalf("收到意外内容: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("订阅通道未收到日志")
	}
}

// TestLogHookSlowConsumerDropped 验证消费慢的订阅者不会阻塞日志流水线
func TestLogHookSlowConsumerDropped(t *testing.T) {
	ch := make(chan string) // 无缓冲且无人消费
	globalLogHook.subsMu.Lock()
	globalLogHook.subs[ch] = struct{}{}
	globalLogHook.subsMu.Unlock()
	defer UnsubscribeLogs(ch)

	entry := logrus.NewEntry(logrus.New())
	entry.Message = "x"
	entry.Level = logrus.InfoLevel
	entry.Time = time.Now()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = GetLogHook().Fire(entry)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Fire 被慢订阅者阻塞")
	}
}
