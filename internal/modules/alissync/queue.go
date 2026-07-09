package alissync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// Save 保存同步任务到磁盘
func (qm *QueueManager) Save(task *SyncTask) error {
	task.UpdatedAt = time.Now()
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

// Load 从磁盘加载单个任务
func (qm *QueueManager) Load(taskID string) (*SyncTask, error) {
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

// Delete 删除任务文件
func (qm *QueueManager) Delete(taskID string) error {
	return os.Remove(qm.taskFilePath(taskID))
}

// LoadAll 加载队列目录中所有任务
func (qm *QueueManager) LoadAll() ([]*SyncTask, error) {
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
		task, err := qm.Load(taskID)
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

// LoadByState 加载指定状态的任务
func (qm *QueueManager) LoadByState(state string) ([]*SyncTask, error) {
	all, err := qm.LoadAll()
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
