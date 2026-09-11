package alistsync

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
	"github.com/sirupsen/logrus"
)

// TestNewAppliesQPSLimit 验证 alistsync 的 qps_limit 配置真实应用到共享客户端
func TestNewAppliesQPSLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Write([]byte(`{"code":200,"message":"ok","data":{"token":"tk"}}`))
		case "/api/me":
			w.Write([]byte(`{"code":200,"message":"ok","data":{"base_path":"/","id":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		ID:       "sync-a",
		URL:      srv.URL,
		Username: "user",
		Password: "pass",
		Retry:    RetryConfig{MaxAttempts: 10, Backoff: "expo"},
		QPSLimit: 7,
	}
	if _, err := New(cfg); err != nil {
		t.Fatalf("New: %v", err)
	}

	// GetClient 返回的是与 New 共享的缓存实例，应能观察到限流策略
	shared, err := alist.GetClient(srv.URL, "user", "pass", "")
	if err != nil {
		t.Fatalf("GetClient: %v", err)
	}
	if got := shared.LimitQPS(); got != 7 {
		t.Fatalf("LimitQPS() = %d, want 7", got)
	}
}

// TestIfNewerWithRealWorldTimeFormat 线上真实格式（+08:00 时区偏移）必须可比，
// 不可比时退化为大小比对，不能一方零时间就永远复制。
func TestIfNewerWithRealWorldTimeFormat(t *testing.T) {
	mk := func(mod string, size int64) *alist.AlistPath {
		return &alist.AlistPath{Modified: mod, Size: size}
	}
	// 源新 → 复制
	if !ShouldOverwrite(OverwriteIfNewer,
		mk("2026-09-08T23:28:50+08:00", 100), mk("2026-09-08T23:19:04+08:00", 100)) {
		t.Fatal("新源应复制")
	}
	// 源旧 → 跳过（原来两边都解析失败变零时间，看似跳过实则误打误撞）
	if ShouldOverwrite(OverwriteIfNewer,
		mk("2026-09-08T23:19:04+08:00", 100), mk("2026-09-08T23:28:50+08:00", 100)) {
		t.Fatal("旧源不应复制")
	}
	// 时间不可比 + 大小一致 → 跳过
	if ShouldOverwrite(OverwriteIfNewer,
		mk("not-a-time", 100), mk("2026-09-08T23:28:50+08:00", 100)) {
		t.Fatal("大小一致应跳过")
	}
	// 时间不可比 + 大小不同 → 复制
	if !ShouldOverwrite(OverwriteIfNewer,
		mk("not-a-time", 101), mk("2026-09-08T23:28:50+08:00", 100)) {
		t.Fatal("大小不同应复制")
	}
}

func TestShouldOverwrite(t *testing.T) {
	src := &alist.AlistPath{Modified: "2026-07-28T10:00:00Z"}
	old := &alist.AlistPath{Modified: "2026-07-27T10:00:00Z"}
	newer := &alist.AlistPath{Modified: "2026-07-29T10:00:00Z"}
	if !ShouldOverwrite(OverwriteAlways, src, newer) {
		t.Fatal("always rejected")
	}
	if !ShouldOverwrite(OverwriteNever, src, nil) {
		t.Fatal("never with missing dst should copy once")
	}
	if ShouldOverwrite(OverwriteNever, src, old) {
		t.Fatal("never with existing dst should skip")
	}
	if !ShouldOverwrite(OverwriteIfNewer, src, old) {
		t.Fatal("newer source rejected")
	}
	if ShouldOverwrite(OverwriteIfNewer, src, newer) {
		t.Fatal("older source accepted")
	}
	// 空策略默认按 if_newer：缺失必同步，避免空配置静默不同步
	if !ShouldOverwrite("", src, nil) {
		t.Fatal("empty policy with nil existing should sync")
	}
	if !ShouldOverwrite("", src, old) {
		t.Fatal("empty policy with older existing should sync")
	}
}

func TestQueueFlatPathChineseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	qm := NewQueueManager(dir, logrus.New())
	task := &SyncTask{
		ID:           "/wo/电视剧/Teach.You.a.Lesson.S01.2026.1080p.NF.WEB-DL.DDP5.1.Atmos.x264-GrassTV/Teach.You.a.Lesson.S01E01.2026.1080p.NF.WEB-DL.DDP5.1.Atmos.x264-GrassTV.mkv",
		SyncConfigID: "wo_sync",
		SrcPath:      "/pt/wo/电视剧/a.mkv",
		DstPath:      "/wo/电视剧/Teach.You.a.Lesson.S01.2026.1080p.NF.WEB-DL.DDP5.1.Atmos.x264-GrassTV/Teach.You.a.Lesson.S01E01.2026.1080p.NF.WEB-DL.DDP5.1.Atmos.x264-GrassTV.mkv",
		State:        "failed",
		CreatedAt:    time.Now(),
	}
	if err := qm.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := qm.Load(task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.DstPath != task.DstPath {
		t.Fatalf("DstPath = %q", got.DstPath)
	}
	all, err := qm.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("LoadAll = %d, want 1", len(all))
	}
}

func TestRetryBackoffGrows(t *testing.T) {
	rand.Seed(1)
	d := NewRetryDaemon(nil, nil, &RetryConfig{MaxAttempts: 10, Backoff: "expo", Jitter: 0}, logrus.New())
	now := time.Now()
	first := d.calcNextRetry(1).Sub(now)
	second := d.calcNextRetry(2).Sub(now)
	if first < 25*time.Second || second <= first {
		t.Fatalf("backoff did not grow: %v %v", first, second)
	}
}
