package alistsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akimio/autofilm/internal/storage"
	"github.com/sirupsen/logrus"
)

// SyncTask 同步任务
type SyncTask struct {
	ID           string    `json:"id"`
	SyncConfigID string    `json:"sync_config_id"`
	SrcPath      string    `json:"src_path"`
	DstPath      string    `json:"dst_path"`
	RawURL       string    `json:"raw_url"`
	AlistTaskID  string    `json:"alist_task_id"`
	State        string    `json:"state"` // pending, running, succeeded, failed, dead_letter
	Attempts     int       `json:"attempts"`
	LastError    string    `json:"last_error"`
	NextRetryAt  time.Time `json:"next_retry_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// QueueManager 重试队列管理器
type QueueManager struct {
	queueDir string
	logger   *logrus.Logger
}

// NewQueueManager 创建队列管理器
func NewQueueManager(queueDir string, logger *logrus.Logger) *QueueManager {
	os.MkdirAll(queueDir, 0755)
	return &QueueManager{queueDir: queueDir, logger: logger}
}

// taskFilePath 返回任务文件路径（扁平化，避免 dstPath 中的 / 形成嵌套目录）
// 命名：sanitized(截断100) + "_" + sha256(dstPath)[:8]，同 dstPath 稳定唯一
func (qm *QueueManager) taskFilePath(taskID string) string {
	trimmed := strings.Trim(taskID, "/")
	safe := strings.ReplaceAll(trimmed, "/", "_")
	safe = strings.ReplaceAll(safe, string(filepath.Separator), "_")
	if len([]rune(safe)) > 100 {
		runes := []rune(safe)
		safe = string(runes[len(runes)-100:])
	}
	if safe == "" {
		safe = "root"
	}
	sum := sha256.Sum256([]byte(taskID))
	return filepath.Join(qm.queueDir, safe+"_"+hex.EncodeToString(sum[:])[:8]+".json")
}

// legacyTaskFilePath 兼容老版本嵌套路径（filepath.Join(queueDir, taskID+".json")）
func (qm *QueueManager) legacyTaskFilePath(taskID string) string {
	return filepath.Join(qm.queueDir, taskID+".json")
}

// Save 保存同步任务
// 优先写入数据库，回退到 JSON 文件；数据库成功后同步更新文件备份
func (qm *QueueManager) Save(task *SyncTask) error {
	task.UpdatedAt = time.Now()
	if store := storage.GlobalStore(); store != nil {
		if err := qm.dbSave(store, task); err != nil {
			return err
		}
		if err := qm.fileSave(task); err != nil {
			qm.logger.Warnf("同步更新任务文件失败 %s: %v", task.ID, err)
		}
		return nil
	}
	return qm.fileSave(task)
}

// Load 加载单个任务
func (qm *QueueManager) Load(taskID string) (*SyncTask, error) {
	if store := storage.GlobalStore(); store != nil {
		return qm.dbLoad(store, taskID)
	}
	return qm.fileLoad(taskID)
}

// Delete 删除任务
func (qm *QueueManager) Delete(taskID string) error {
	if store := storage.GlobalStore(); store != nil {
		return store.DeleteSyncTaskByDstPath(taskID)
	}
	return qm.fileDelete(taskID)
}

// LoadAll 加载所有任务
func (qm *QueueManager) LoadAll() ([]*SyncTask, error) {
	if store := storage.GlobalStore(); store != nil {
		return qm.dbLoadAll(store)
	}
	return qm.fileLoadAll()
}

// LoadByState 加载指定状态的任务
func (qm *QueueManager) LoadByState(state string) ([]*SyncTask, error) {
	if store := storage.GlobalStore(); store != nil {
		rows, err := store.ListSyncTasksByState(state)
		if err != nil {
			return nil, err
		}
		return syncTaskRowsToTasks(rows), nil
	}
	return qm.fileLoadByState(state)
}

// ==================== DB 实现 ====================

func (qm *QueueManager) dbSave(store *storage.Store, task *SyncTask) error {
	return store.UpsertSyncTask(&storage.SyncTaskRow{
		SyncConfigID: 0,
		ConfigUID:    task.SyncConfigID,
		SrcPath:      task.SrcPath,
		DstPath:      task.DstPath,
		State:        task.State,
		AlistTaskID:  task.AlistTaskID,
		Attempts:     task.Attempts,
		LastError:    task.LastError,
		NextRetryAt:  &task.NextRetryAt,
	})
}

func (qm *QueueManager) dbLoad(store *storage.Store, dstPath string) (*SyncTask, error) {
	row, err := store.GetSyncTaskByDstPath(dstPath)
	if err != nil {
		return nil, err
	}
	return syncTaskRowToTask(row), nil
}

func (qm *QueueManager) dbLoadAll(store *storage.Store) ([]*SyncTask, error) {
	rows, err := store.ListAllSyncTasks()
	if err != nil {
		return nil, err
	}
	return syncTaskRowsToTasks(rows), nil
}

func syncTaskRowToTask(r *storage.SyncTaskRow) *SyncTask {
	t := &SyncTask{
		ID:           r.DstPath,
		SyncConfigID: r.ConfigUID,
		SrcPath:      r.SrcPath,
		DstPath:      r.DstPath,
		State:        r.State,
		AlistTaskID:  r.AlistTaskID,
		Attempts:     r.Attempts,
		LastError:    r.LastError,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	if r.NextRetryAt != nil {
		t.NextRetryAt = *r.NextRetryAt
	}
	return t
}

func syncTaskRowsToTasks(rows []*storage.SyncTaskRow) []*SyncTask {
	tasks := make([]*SyncTask, len(rows))
	for i, r := range rows {
		tasks[i] = syncTaskRowToTask(r)
	}
	return tasks
}

// ==================== JSON 文件实现 ====================

func (qm *QueueManager) fileSave(task *SyncTask) error {
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	path := qm.taskFilePath(task.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (qm *QueueManager) fileLoad(taskID string) (*SyncTask, error) {
	path := qm.taskFilePath(taskID)
	data, err := os.ReadFile(path)
	if err != nil && os.IsNotExist(err) {
		// 兼容老版本嵌套文件
		data, err = os.ReadFile(qm.legacyTaskFilePath(taskID))
	}
	if err != nil {
		return nil, err
	}
	var task SyncTask
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (qm *QueueManager) fileDelete(taskID string) error {
	err1 := os.Remove(qm.taskFilePath(taskID))
	err2 := os.Remove(qm.legacyTaskFilePath(taskID))
	if err1 != nil && err2 != nil {
		return err1
	}
	return nil
}

func (qm *QueueManager) fileLoadAll() ([]*SyncTask, error) {
	if _, err := os.Stat(qm.queueDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tasks []*SyncTask
	// 递归兼容：新版扁平文件 + 老版嵌套子目录文件
	err := filepath.WalkDir(qm.queueDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") || strings.HasSuffix(d.Name(), ".tmp") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var task SyncTask
		if err := json.Unmarshal(data, &task); err != nil {
			return nil
		}
		tasks = append(tasks, &task)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks, nil
}

func (qm *QueueManager) fileLoadByState(state string) ([]*SyncTask, error) {
	all, err := qm.fileLoadAll()
	if err != nil {
		return nil, err
	}
	var filtered []*SyncTask
	for _, t := range all {
		if t.State == state {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}
