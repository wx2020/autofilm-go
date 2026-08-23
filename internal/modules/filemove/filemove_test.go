package filemove

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
