package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/akimio/autofilm/internal/web"

	_ "modernc.org/sqlite"
)

func setupMainTestStore(t *testing.T) *storage.Store {
	t.Helper()

	tmp, err := os.CreateTemp("", "main_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	t.Cleanup(func() { os.Remove(tmp.Name()) })

	db, err := sql.Open("sqlite", tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if err := storage.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	store := storage.NewStore(db, make([]byte, 32))
	old := storage.GlobalStore()
	storage.SetGlobalStore(store)
	t.Cleanup(func() { storage.SetGlobalStore(old) })
	return store
}

func waitForCondition(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cond()
}

// TestRunOnStartOnlyFiresAtStartup 验证 run_on_start 语义：
// fireRunOnStart=true（进程启动）时触发立即执行；
// fireRunOnStart=false（配置热重载）时只注册 cron，不再重复触发。
// 回归场景：点 A 的运行 / 切换开关 / 保存配置时，run_on_start 的 B 被意外带起。
func TestRunOnStartOnlyFiresAtStartup(t *testing.T) {
	core.InitLogger()
	logger = core.GetLogger()

	// 所有请求直接 500，让任务快速失败，便于观察“是否被触发”
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"code":500,"message":"boom"}`))
	}))
	defer srv.Close()

	store := setupMainTestStore(t)
	cfg := map[string]interface{}{
		"id": "ros-task", "enable": true, "run_on_start": true,
		"url": srv.URL, "username": "u", "password": "p",
		"source_dir": "/", "target_dir": t.TempDir(),
		"cron": "0 0 * * * *",
	}
	if err := store.SaveModuleConfig("alist2strm", cfg); err != nil {
		t.Fatalf("SaveModuleConfig: %v", err)
	}
	countRuns := func() int {
		runs, err := store.ListRecentTaskRuns(100)
		if err != nil {
			t.Fatalf("ListRecentTaskRuns: %v", err)
		}
		return len(runs)
	}

	// 热重载路径：只注册，不触发立即执行
	addAlist2StrmJobs(newScheduler(), false)
	time.Sleep(2 * time.Second)
	if n := countRuns(); n != 0 {
		t.Fatalf("热重载触发了 run_on_start，运行记录 = %d, want 0", n)
	}
	if e := web.GetModuleRegistry().Get(web.ModuleAlist2Strm, "ros-task"); e == nil {
		t.Fatal("热重载后任务应已注册（cron 保留）")
	}

	// 进程启动路径：触发一次立即执行（目标 500 会快速失败并留下运行记录）
	addAlist2StrmJobs(newScheduler(), true)
	if !waitForCondition(15*time.Second, func() bool { return countRuns() >= 1 }) {
		t.Fatal("进程启动未触发 run_on_start，运行记录始终为 0")
	}

	web.GetModuleRegistry().Unregister(web.ModuleAlist2Strm, "ros-task")
	web.GetModuleRegistry().ReleaseRun(web.ModuleAlist2Strm, "ros-task")
}
