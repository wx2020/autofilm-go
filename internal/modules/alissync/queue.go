package alissync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akimio/autofilm/internal/storage"
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
}

// NewQueueManager 创建队列管理器
func NewQueueManager(queueDir string) *QueueManager {
	os.MkdirAll(queueDir, 0755)
	return &QueueManager{queueDir: queueDir}
}

// taskFilePath 返回任务文件路径
func (qm *QueueManager) taskFilePath(taskID string) string {
	return filepath.Join(qm.queueDir, taskID+".json")
}

// Save 保存同步任务
// 优先写入数据库，回退到 JSON 文件
func (qm *QueueManager) Save(task *SyncTask) error {
	task.UpdatedAt = time.Now()
	if store := storage.GlobalStore(); store != nil {
		return qm.dbSave(store, task)
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
		SyncConfigID: strconv.FormatInt(r.SyncConfigID, 10),
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
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (qm *QueueManager) fileLoad(taskID string) (*SyncTask, error) {
	data, err := os.ReadFile(qm.taskFilePath(taskID))
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
	return os.Remove(qm.taskFilePath(taskID))
}

func (qm *QueueManager) fileLoadAll() ([]*SyncTask, error) {
	entries, err := os.ReadDir(qm.queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var tasks []*SyncTask
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		taskID := strings.TrimSuffix(entry.Name(), ".json")
		task, err := qm.fileLoad(taskID)
		if err != nil {
			continue
		}
		tasks = append(tasks, task)
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
