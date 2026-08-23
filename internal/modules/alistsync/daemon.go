package alistsync

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
	"github.com/sirupsen/logrus"
)

// RetryDaemon 守护重试协程
type RetryDaemon struct {
	client      *alist.AlistClient
	queue       *QueueManager
	config      *RetryConfig
	logger      *logrus.Logger
	stopCh      chan struct{}
	wg          sync.WaitGroup
	activeTasks map[string]*SyncTask
	activeMu    sync.RWMutex
}

// NewRetryDaemon 创建守护重试协程
func NewRetryDaemon(client *alist.AlistClient, queue *QueueManager, config *RetryConfig, logger *logrus.Logger) *RetryDaemon {
	return &RetryDaemon{
		client:      client,
		queue:       queue,
		config:      config,
		logger:      logger,
		stopCh:      make(chan struct{}),
		activeTasks: make(map[string]*SyncTask),
	}
}

// Start 启动守护协程
func (d *RetryDaemon) Start(ctx context.Context) {
	d.wg.Add(1)
	go d.loop(ctx)
}

// Stop 停止守护协程
func (d *RetryDaemon) Stop() {
	close(d.stopCh)
	d.wg.Wait()
}

// AddTask 添加任务到守护协程跟踪
func (d *RetryDaemon) AddTask(task *SyncTask) {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()
	d.activeTasks[task.ID] = task
}

// RemoveTask 从守护协程移除任务
func (d *RetryDaemon) RemoveTask(taskID string) {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()
	delete(d.activeTasks, taskID)
}

func (d *RetryDaemon) loop(ctx context.Context) {
	defer d.wg.Done()

	d.logger.Info("同步守护协程已启动")

	// 启动时加载磁盘上所有未完成的任务
	d.loadPendingTasks()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			d.logger.Info("同步守护协程已停止")
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.pollTasks(ctx)
		}
	}
}

func (d *RetryDaemon) loadPendingTasks() {
	tasks, err := d.queue.LoadAll()
	if err != nil {
		d.logger.Errorf("加载队列任务失败: %v", err)
		return
	}

	d.activeMu.Lock()
	for _, task := range tasks {
		if task.State == "succeeded" {
			continue
		}
		d.activeTasks[task.ID] = task
	}
	d.activeMu.Unlock()

	d.logger.Infof("已从磁盘恢复 %d 个同步任务", len(d.activeTasks))
}

func (d *RetryDaemon) pollTasks(ctx context.Context) {
	d.activeMu.RLock()
	tasks := make([]*SyncTask, 0, len(d.activeTasks))
	for _, t := range d.activeTasks {
		tasks = append(tasks, t)
	}
	d.activeMu.RUnlock()

	for _, task := range tasks {
		select {
		case <-d.stopCh:
			return
		default:
		}

		switch task.State {
		case "running":
			d.checkRunningTask(ctx, task)
		case "failed":
			d.checkRetryTask(ctx, task)
		}
	}
}

func (d *RetryDaemon) checkRunningTask(ctx context.Context, task *SyncTask) {
	if task.AlistTaskID == "" {
		return
	}

	info, err := d.client.TaskInfo(ctx, task.AlistTaskID)
	if err != nil {
		d.logger.Debugf("查询任务状态失败 %s: %v", task.AlistTaskID, err)
		return
	}

	switch info.State {
	case "succeeded":
		task.State = "succeeded"
		task.LastError = ""
		if err := d.queue.Save(task); err != nil {
			d.logger.Errorf("保存任务状态失败 %s: %v", task.ID, err)
		}
		d.logger.Infof("同步任务完成: %s -> %s", task.SrcPath, task.DstPath)
		d.RemoveTask(task.ID)

	case "failed", "canceled":
		task.Attempts++
		task.LastError = info.Status
		if d.isMaxAttemptsExceeded(task) {
			task.State = "dead_letter"
			d.logger.Warnf("同步任务超过最大重试次数: %s -> %s, 错误: %s",
				task.SrcPath, task.DstPath, info.Status)
		} else {
			task.State = "failed"
			task.NextRetryAt = d.calcNextRetry(task.Attempts)
			d.logger.Infof("同步任务失败，将在 %v 后重试 (第 %d 次): %s",
				time.Until(task.NextRetryAt), task.Attempts, task.SrcPath)
		}
		if err := d.queue.Save(task); err != nil {
			d.logger.Errorf("保存任务状态失败 %s: %v", task.ID, err)
		}
	}
}

func (d *RetryDaemon) checkRetryTask(ctx context.Context, task *SyncTask) {
	if time.Now().Before(task.NextRetryAt) {
		return
	}

	d.logger.Infof("正在重试同步任务: %s -> %s (第 %d 次)", task.SrcPath, task.DstPath, task.Attempts+1)

	if err := d.client.TaskRetry(ctx, task.AlistTaskID); err != nil {
		d.logger.Warnf("TaskRetry 失败 %s, 尝试重新提交: %v", task.AlistTaskID, err)

		newTaskID, err := d.client.FSPut(ctx, dstDirFromPath(task.DstPath), []alist.FSPutFile{
			{Path: fileNameFromPath(task.DstPath), URL: task.RawURL},
		})
		if err != nil {
			task.Attempts++
			task.LastError = err.Error()
			task.NextRetryAt = d.calcNextRetry(task.Attempts)
			if d.isMaxAttemptsExceeded(task) {
				task.State = "dead_letter"
				d.logger.Errorf("同步任务超过最大重试次数: %s", task.SrcPath)
			}
			d.queue.Save(task)
			return
		}
		task.AlistTaskID = newTaskID
	}

	task.State = "running"
	task.LastError = ""
	if err := d.queue.Save(task); err != nil {
		d.logger.Errorf("保存任务状态失败 %s: %v", task.ID, err)
	}
}

func (d *RetryDaemon) isMaxAttemptsExceeded(task *SyncTask) bool {
	maxAttempts := d.config.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	return task.Attempts >= maxAttempts
}

func (d *RetryDaemon) calcNextRetry(attempt int) time.Time {
	if d.config.Backoff != "expo" {
		d.config.Backoff = "expo"
	}

	base := 30 * time.Second
	backoff := time.Duration(float64(base) * math.Pow(2, float64(attempt-1)))

	if d.config.Jitter > 0 {
		jitterRange := time.Duration(float64(backoff) * d.config.Jitter)
		if jitterRange > 0 {
			jitter := time.Duration(rand.Int63n(int64(jitterRange*2))) - jitterRange
			backoff += jitter
		}
	}

	maxBackoff := 30 * time.Minute
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	if backoff < base {
		backoff = base
	}

	return time.Now().Add(backoff)
}

// 辅助函数
func dstDirFromPath(dstPath string) string {
	for i := len(dstPath) - 1; i >= 0; i-- {
		if dstPath[i] == '/' {
			return dstPath[:i]
		}
	}
	return dstPath
}

func fileNameFromPath(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
