package storage

import (
	"database/sql"
	"os"
	"sync"
	"testing"
)

func setupTestDB(t *testing.T) (*Store, func()) {
	tmpFile, err := os.CreateTemp("", "test_db_*.db")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatal(err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS alist2strm_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		config_id INTEGER NOT NULL,
		path TEXT NOT NULL,
		size INTEGER,
		modified TEXT,
		sign TEXT
	)`)
	if err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatal(err)
	}

	store := &Store{db: db}

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}

	return store, cleanup
}

func TestSaveSnapshotNoDataLoss(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	configID := int64(1)
	entries := []SnapshotEntry{
		{ConfigID: configID, Path: "/file1.txt", Size: 100, Modified: "2024-01-01", Sign: "sig1"},
		{ConfigID: configID, Path: "/file2.txt", Size: 200, Modified: "2024-01-02", Sign: "sig2"},
		{ConfigID: configID, Path: "/file3.txt", Size: 300, Modified: "2024-01-03", Sign: "sig3"},
	}

	err := store.SaveSnapshot(configID, entries)
	if err != nil {
		t.Fatalf("SaveSnapshot 失败: %v", err)
	}

	loaded, err := store.LoadSnapshot(configID)
	if err != nil {
		t.Fatalf("LoadSnapshot 失败: %v", err)
	}

	if len(loaded) != len(entries) {
		t.Errorf("期望 %d 条快照，实际加载 %d 条", len(entries), len(loaded))
	}

	for i, e := range entries {
		if loaded[i].Path != e.Path {
			t.Errorf("快照 %d: 路径不匹配，期望 %s，实际 %s", i, e.Path, loaded[i].Path)
		}
	}
}

func TestSaveSnapshotConcurrentUpdate(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	configID := int64(1)

	entries := []SnapshotEntry{
		{ConfigID: configID, Path: "/file1.txt", Size: 100, Modified: "2024-01-01", Sign: "sig1"},
		{ConfigID: configID, Path: "/file2.txt", Size: 200, Modified: "2024-01-02", Sign: "sig2"},
	}

	err := store.SaveSnapshot(configID, entries)
	if err != nil {
		t.Fatalf("初始 SaveSnapshot 失败: %v", err)
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			newEntries := []SnapshotEntry{
				{ConfigID: configID, Path: "/file1.txt", Size: int64(100 + idx), Modified: "2024-01-01", Sign: "sig1"},
				{ConfigID: configID, Path: "/file2.txt", Size: int64(200 + idx), Modified: "2024-01-02", Sign: "sig2"},
			}
			if err := store.SaveSnapshot(configID, newEntries); err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("并发 SaveSnapshot 失败: %v", err)
	}

	loaded, err := store.LoadSnapshot(configID)
	if err != nil {
		t.Fatalf("LoadSnapshot 失败: %v", err)
	}

	if len(loaded) != len(entries) {
		t.Errorf("并发更新后，期望 %d 条快照，实际加载 %d 条，数据丢失", len(entries), len(loaded))
	}
}

func TestSaveSnapshotPartialFailure(t *testing.T) {
	store, cleanup := setupTestDB(t)
	defer cleanup()

	configID := int64(1)
	entries := []SnapshotEntry{
		{ConfigID: configID, Path: "/file1.txt", Size: 100, Modified: "2024-01-01", Sign: "sig1"},
		{ConfigID: configID, Path: "/file2.txt", Size: 200, Modified: "2024-01-02", Sign: "sig2"},
	}

	err := store.SaveSnapshot(configID, entries)
	if err != nil {
		t.Fatalf("初始 SaveSnapshot 失败: %v", err)
	}

	entries2 := []SnapshotEntry{
		{ConfigID: configID, Path: "/file1.txt", Size: 100, Modified: "2024-01-01", Sign: "sig1"},
		{ConfigID: configID, Path: "/file2.txt", Size: 200, Modified: "2024-01-02", Sign: "sig2"},
		{ConfigID: configID, Path: "/file3.txt", Size: 300, Modified: "2024-01-03", Sign: "sig3"},
	}

	err = store.SaveSnapshot(configID, entries2)
	if err != nil {
		t.Fatalf("第二次 SaveSnapshot 失败: %v", err)
	}

	loaded, err := store.LoadSnapshot(configID)
	if err != nil {
		t.Fatalf("LoadSnapshot 失败: %v", err)
	}

	if len(loaded) != len(entries2) {
		t.Errorf("期望 %d 条快照（包含新增的 file3），实际加载 %d 条", len(entries2), len(loaded))
	}

	found := false
	for _, e := range loaded {
		if e.Path == "/file3.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Error("file3.txt 应该在快照中但找不到，存在数据丢失")
	}
}
