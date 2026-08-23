package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ImportYamlConfigs 从 yaml 配置导入模块配置到数据库
// 使用 core.Store() 读取各模块列表，逐条导入
func ImportYamlConfigs(s *Store) error {
	importers := []struct {
		moduleType string
		list       []map[string]interface{}
		getter     func() []map[string]interface{}
	}{
		{"alist2strm", nil, getYamlList("Alist2StrmList")},
		{"ani2alist", nil, getYamlList("Ani2AlistList")},
		{"alistsync", nil, getYamlList("AlistSyncList")},
		{"filemove", nil, getYamlList("FileMoveList")},
	}

	for _, imp := range importers {
		list := imp.getter()
		for _, cfg := range list {
			if err := s.ImportConfigMap(imp.moduleType, cfg); err != nil {
				return fmt.Errorf("导入 %s 配置失败: %w", imp.moduleType, err)
			}
		}
	}

	return nil
}

func getYamlList(key string) func() []map[string]interface{} {
	return func() []map[string]interface{} {
		// 直接从 viper 读取（通过内部 core 包的访问器）
		// 实际调用在 main.go 阶段完成，这里作为接口占位
		return nil
	}
}

// ImportJsonSnapshots 从 config/cache/ 导入旧快照到数据库
func ImportJsonSnapshots(s *Store, cacheDir string) error {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "alist2strm_") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(cacheDir, entry.Name()))
		if err != nil {
			continue
		}

		var snap struct {
			Files map[string]struct {
				Size     int64  `json:"size"`
				Modified string `json:"modified"`
				Sign     string `json:"sign"`
			} `json:"files"`
		}
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}

		// cfg_id 从文件名提取: alist2strm_<id>.json
		idStr := strings.TrimPrefix(entry.Name(), "alist2strm_")
		idStr = strings.TrimSuffix(idStr, ".json")

		// 查找对应的 config_id
		var configID int64
		err = s.db.QueryRow("SELECT id FROM alist2strm_configs WHERE cfg_id = ?", idStr).Scan(&configID)
		if err != nil {
			continue
		}

		var entries []SnapshotEntry
		for path, fe := range snap.Files {
			entries = append(entries, SnapshotEntry{
				ConfigID: configID,
				Path:     path,
				Size:     fe.Size,
				Modified: fe.Modified,
				Sign:     fe.Sign,
			})
		}

		if len(entries) > 0 {
			s.SaveSnapshot(configID, entries)
		}
	}

	return nil
}

// ImportJsonSyncQueue 从 config/sync_queue/ 导入旧同步队列到数据库
func ImportJsonSyncQueue(s *Store, queueDir string) error {
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(queueDir, entry.Name()))
		if err != nil {
			continue
		}

		var t SyncTaskRow
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}

		s.CreateSyncTask(&t)
	}

	return nil
}
