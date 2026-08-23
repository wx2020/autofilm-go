package alistsync

import (
	"math/rand"
	"testing"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
	"github.com/sirupsen/logrus"
)

func TestShouldOverwrite(t *testing.T) {
	src := &alist.AlistPath{Modified: "2026-07-28T10:00:00Z"}
	old := &alist.AlistPath{Modified: "2026-07-27T10:00:00Z"}
	newer := &alist.AlistPath{Modified: "2026-07-29T10:00:00Z"}
	if !ShouldOverwrite(OverwriteAlways, src, newer) {
		t.Fatal("always rejected")
	}
	if ShouldOverwrite(OverwriteNever, src, nil) {
		t.Fatal("never accepted")
	}
	if !ShouldOverwrite(OverwriteIfNewer, src, old) {
		t.Fatal("newer source rejected")
	}
	if ShouldOverwrite(OverwriteIfNewer, src, newer) {
		t.Fatal("older source accepted")
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
