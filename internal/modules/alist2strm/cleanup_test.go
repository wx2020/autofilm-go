package alist2strm

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

func newTestA2S(t *testing.T) (*Alist2Strm, string) {
	t.Helper()
	target := t.TempDir()
	return &Alist2Strm{
		config: &Config{
			ID:        "test",
			SourceDir: "/remote",
			TargetDir: target,
		},
		processedPaths: make(map[string]struct{}),
		logger:         logrus.New(),
	}, target
}

// TestCalcQPSDefaults QPS 计算策略：显式配置优先，自动模式 = max(workers/2,1) 封顶 10
func TestCalcQPSDefaults(t *testing.T) {
	cases := []struct{ limit, workers, want int }{
		{0, 50, 10},
		{0, 4, 2},
		{0, 1, 1},
		{30, 50, 30},
		{0, 0, 1},
	}
	for _, c := range cases {
		a := &Alist2Strm{config: &Config{QPSLimit: c.limit, MaxWorkers: c.workers}}
		if got := a.calcQPS(); got != c.want {
			t.Errorf("calcQPS(limit=%d, workers=%d) = %d, want %d", c.limit, c.workers, got, c.want)
		}
	}
}

// TestCleanupSkipsWhenNothingSeenFromRemote 数据源异常（远端列表为空）时不得删除本地 .strm
func TestCleanupSkipsWhenNothingSeenFromRemote(t *testing.T) {
	a2s, target := newTestA2S(t)
	strm := filepath.Join(target, "movie.strm")
	if err := os.WriteFile(strm, []byte("http://example.com/d/movie.mkv"), 0644); err != nil {
		t.Fatal(err)
	}

	// processedPaths 为空：模拟远端未返回任何文件
	if err := a2s.cleanupLocalFiles(context.Background()); err == nil {
		t.Fatal("远端结果为空时应返回错误并跳过清理")
	}
	if _, err := os.Stat(strm); err != nil {
		t.Fatal("本地 .strm 被误删")
	}
}

// TestCleanupRemovesStaleFiles 正常扫描后仍应清理远端已不存在的文件
func TestCleanupRemovesStaleFiles(t *testing.T) {
	a2s, target := newTestA2S(t)
	keep := filepath.Join(target, "keep.strm")
	stale := filepath.Join(target, "stale.strm")
	for _, p := range []string{keep, stale} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	a2s.addProcessedPath(keep)

	if err := a2s.cleanupLocalFiles(context.Background()); err != nil {
		t.Fatalf("cleanupLocalFiles: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("已处理文件不应被删除")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("过期文件应被删除")
	}
}
