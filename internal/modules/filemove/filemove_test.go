package filemove

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewOpenListAppliesQPSLimit 验证 filemove openlist 后端的 qps_limit 配置真实生效
func TestNewOpenListAppliesQPSLimit(t *testing.T) {
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

	mover, err := New(&Config{
		ID:        "fm-a",
		Backend:   "openlist",
		URL:       srv.URL,
		Username:  "user",
		Password:  "pass",
		SourceDir: "/source",
		TargetDir: "/target",
		QPSLimit:  4,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mover.client == nil {
		t.Fatal("openlist 后端应创建客户端")
	}
	if got := mover.client.LimitQPS(); got != 4 {
		t.Fatalf("LimitQPS() = %d, want 4", got)
	}
}

func TestMoveMatchesRegexAndSizePreservesRelativePath(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "nested", "movie.mkv"), "12345")
	writeTestFile(t, filepath.Join(source, "nested", "movie.txt"), "12345")
	writeTestFile(t, filepath.Join(source, "nested", "small.mkv"), "12")

	mover, err := New(&Config{
		SourceDir: source,
		TargetDir: target,
		Regex:     `(?i)\.mkv$`,
		MinSize:   5,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := mover.Move(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Scanned != 3 || report.Matched != 1 || report.Moved != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(target, "nested", "movie.mkv")); err != nil {
		t.Fatalf("matched file was not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, "nested", "movie.txt")); err != nil {
		t.Fatalf("non-matching file was moved or removed: %v", err)
	}
}

func TestMoveExactSizeAndSkipsExistingDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "a.bin"), "1234")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(target, "a.bin"), "existing")

	size := int64(4)
	mover, err := New(&Config{SourceDir: source, TargetDir: target, Size: &size})
	if err != nil {
		t.Fatal(err)
	}
	report, err := mover.Move(context.Background())
	if err != nil {
		t.Fatalf("destination conflict should be skipped without failing the scan: %v", err)
	}
	if report.Moved != 0 || report.Skipped != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	content, readErr := os.ReadFile(filepath.Join(target, "a.bin"))
	if readErr != nil || string(content) != "existing" {
		t.Fatalf("destination was overwritten: %q, %v", content, readErr)
	}
}

func TestMoveFlattenPreservesOnlyFileName(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "nested", "deep"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "nested", "deep", "movie.mkv"), "movie")

	mover, err := New(&Config{SourceDir: source, TargetDir: target, Flatten: true})
	if err != nil {
		t.Fatal(err)
	}
	report, err := mover.Move(context.Background())
	if err != nil || report.Moved != 1 {
		t.Fatalf("flatten move failed: report=%+v err=%v", report, err)
	}
	if _, err := os.Stat(filepath.Join(target, "movie.mkv")); err != nil {
		t.Fatalf("flattened file was not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "nested", "deep", "movie.mkv")); !os.IsNotExist(err) {
		t.Fatalf("flattened file retained source directories")
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"50KB":       50 * 1024,
		"500MB":      500 * 1024 * 1024,
		"1GB":        1024 * 1024 * 1024,
		"1073741824": 1024 * 1024 * 1024,
	}
	for input, expected := range cases {
		got, err := ParseSize(input)
		if err != nil || got != expected {
			t.Fatalf("ParseSize(%q) = %d, %v; want %d", input, got, err, expected)
		}
	}
}

func TestMoveRenamesFileBeforeMove(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "hhd880.com@ipx111-c.mp4"), "video")

	mover, err := New(&Config{
		SourceDir:         source,
		TargetDir:         target,
		RenameRegex:       `^hhd880\.com@`,
		RenameReplacement: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := mover.Move(context.Background())
	if err != nil || report.Renamed != 1 || report.Moved != 1 {
		t.Fatalf("rename move failed: report=%+v err=%v", report, err)
	}
	if _, err := os.Stat(filepath.Join(target, "ipx111-c.mp4")); err != nil {
		t.Fatalf("renamed file was not moved: %v", err)
	}
}

func TestMoveRemovesMatchedDirectoryAfterAllFilesMove(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	dir := filepath.Join(source, "download")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "a.mp4"), "a")
	writeTestFile(t, filepath.Join(dir, "b.mp4"), "b")
	writeTestFile(t, filepath.Join(dir, "resume.part"), "garbage")

	mover, err := New(&Config{
		SourceDir:         source,
		TargetDir:         target,
		Regex:             `\.mp4$`,
		RemoveMatchedDirs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := mover.Move(context.Background())
	if err != nil || report.Matched != 2 || report.Moved != 2 || report.RemovedDirs != 1 {
		t.Fatalf("matched directory cleanup failed: report=%+v err=%v", report, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("matched source directory was not removed: %v", err)
	}
	for _, name := range []string{"a.mp4", "b.mp4"} {
		if _, err := os.Stat(filepath.Join(target, "download", name)); err != nil {
			t.Fatalf("moved file %s is missing: %v", name, err)
		}
	}
}

func TestMoveKeepsMatchedDirectoryWhenOneFileFails(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	dir := filepath.Join(source, "download")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "a.mp4"), "a")
	writeTestFile(t, filepath.Join(dir, "b.mp4"), "b")
	if err := os.MkdirAll(filepath.Join(target, "download"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(target, "download", "b.mp4"), "existing")

	mover, err := New(&Config{SourceDir: source, TargetDir: target, Regex: `\.mp4$`, RemoveMatchedDirs: true})
	if err != nil {
		t.Fatal(err)
	}
	report, err := mover.Move(context.Background())
	if err != nil || report.Matched != 2 || report.Moved != 1 || report.RemovedDirs != 0 {
		t.Fatalf("failed-file cleanup guard failed: report=%+v err=%v", report, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("matched directory was removed after a failed move: %v", err)
	}
}

func TestNewRejectsTargetInsideSource(t *testing.T) {
	root := t.TempDir()
	_, err := New(&Config{
		SourceDir: root,
		TargetDir: filepath.Join(root, "moved"),
	})
	if err == nil || !strings.Contains(err.Error(), "inside source_dir") {
		t.Fatalf("expected nested target rejection, got %v", err)
	}
}

func TestMoveHonorsContext(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mover, err := New(&Config{SourceDir: source, TargetDir: filepath.Join(root, "target")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mover.Move(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
