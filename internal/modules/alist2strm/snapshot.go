package alist2strm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
)

// FileEntry 快照中单个文件的元数据
type FileEntry struct {
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Sign     string `json:"sign"`
}

// Snapshot 目录快照
type Snapshot struct {
	Updated time.Time            `json:"updated"`
	Files   map[string]FileEntry `json:"files"`
}

// BuildSnapshot 从文件列表构建快照
func BuildSnapshot(files []alist.AlistPath) *Snapshot {
	snap := &Snapshot{
		Updated: time.Now(),
		Files:   make(map[string]FileEntry, len(files)),
	}
	for _, f := range files {
		snap.Files[f.FullPath] = FileEntry{
			Size:     f.Size,
			Modified: f.Modified,
			Sign:     f.Sign,
		}
	}
	return snap
}

// DiffSnapshots 对比新旧快照，返回新增、修改、删除的文件路径列表
// modified 判定条件：size 或 modified 任一发生变化
func DiffSnapshots(old, new *Snapshot) (added, modified, deleted []string) {
	for path, newEntry := range new.Files {
		oldEntry, exists := old.Files[path]
		if !exists {
			added = append(added, path)
		} else if oldEntry.Size != newEntry.Size || oldEntry.Modified != newEntry.Modified {
			modified = append(modified, path)
		}
	}
	for path := range old.Files {
		if _, exists := new.Files[path]; !exists {
			deleted = append(deleted, path)
		}
	}
	sort.Strings(added)
	sort.Strings(modified)
	sort.Strings(deleted)
	return
}

// snapshotFilePath 返回快照文件路径
func snapshotFilePath(cacheDir, id string) string {
	return filepath.Join(cacheDir, "alist2strm_"+id+".json")
}

// LoadSnapshot 从磁盘加载快照，文件不存在或损坏时返回 nil
func LoadSnapshot(id, cacheDir string) (*Snapshot, error) {
	path := snapshotFilePath(cacheDir, id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	if snap.Files == nil {
		snap.Files = make(map[string]FileEntry)
	}
	return &snap, nil
}

// SaveSnapshot 将快照原子写入磁盘
func SaveSnapshot(id, cacheDir string, snap *Snapshot) error {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}

	path := snapshotFilePath(cacheDir, id)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
