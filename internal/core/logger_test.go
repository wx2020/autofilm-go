package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDatedLogPath(t *testing.T) {
	got := DatedLogPath(filepath.Join("logs", "AutoFilm.log"), time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	want := filepath.Join("logs", "AutoFilm-2026-07-22.log")
	if got != want {
		t.Fatalf("DatedLogPath() = %q, want %q", got, want)
	}
}
