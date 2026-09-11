package alistsync

import (
	"context"
	"fmt"
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
	startOnce   sync.Once
	stopOnce    sync.Once
	wg          sync.WaitGroup
	activeTasks map[string]*SyncTask
	activeMu    sync.RWMutex
}

// liveDaemons 存活守护协程表：Web 删除队列任务时需同步移出内存表，
// 否则下轮 poll 会把已删行重新写活。
var (
	liveDaemonsMu sync.Mutex
	liveDaemons   = map[*RetryDaemon]struct{}{}
)

// DropTaskFromDaemons 从所有存活守护协程的内存表中移除任务（Web 删除队列任务时调用）
func DropTaskFromDaemons(taskID string) {
	liveDaemonsMu.Lock()
	daemons := make([]*RetryDaemon, 0, len(liveDaemons))
	for d := range liveDaemons {
		daemons = append(daemons, d)
	}
	liveDaemonsMu.Unlock()
	for _, d := range daemons {
		d.RemoveTask(taskID)
	}
}

// NewRetryDaemon 创建守护重试协程
func NewRetryDaemon(client *alist.AlistClient, queue *QueueManager, config *RetryConfig, logger *logrus.Logger) *RetryDaemon {
	d := &RetryDaemon{
		client:      client,
		queue:       queue,
		config:      config,
		logger:      logger,
		stopCh:      make(chan struct{}),
		activeTasks: make(map[string]*SyncTask),
	}
	liveDaemonsMu.Lock()
	liveDaemons[d] = struct{}{}
	liveDaemonsMu.Unlock()
	return d
}

// Start 启动守护协程（幂等：重复调用不会启动第二个轮询循环）
func (d *RetryDaemon) Start(ctx context.Context) {
	d.startOnce.Do(func() {
		d.wg.Add(1)
		go d.loop(ctx)
	})
}

// Stop 停止守护协程（幂等，可安全多次调用）
func (d *RetryDaemon) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopCh)
	})
	d.wg.Wait()
	liveDaemonsMu.Lock()
	delete(liveDaemons, d)
	liveDaemonsMu.Unlock()
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
	restored := len(d.activeTasks)
	d.activeMu.Unlock()

	d.logger.Infof("已从磁盘恢复 %d 个同步任务", restored)
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
		case "pending", "running":
			d.checkPendingTask(ctx, task)
		case "failed":
			d.checkRetryTask(ctx, task)
		}
	}
}

func (d *RetryDaemon) checkPendingTask(ctx context.Context, task *SyncTask) {
	if task.AlistTaskID == "" {
		return
	}

	info, err := d.client.TaskInfo(ctx, task.AlistTaskID)
	if err != nil {
		d.logger.Debugf("查询任务状态失败 %s: %v", task.AlistTaskID, err)
		return
	}

	// v4 任务状态为 tache 数字（2=成功），用 TaskTerminal 兼容新旧两种形态
	terminal, success := alist.TaskTerminal(info.State)
	if !terminal {
		return
	}
	if success {
		if _, err := d.client.FSGet(ctx, task.DstPath); err != nil {
			d.logger.Warnf("Alist 报告成功但目标文件不存在: %s, 错误: %v", task.DstPath, err)
			task.Attempts++
			task.LastError = fmt.Sprintf("目标文件验证失败: %v", err)
			task.State = "failed"
			task.NextRetryAt = d.calcNextRetry(task.Attempts)
			if err := d.queue.Save(task); err != nil {
				d.logger.Errorf("保存任务状态失败 %s: %v", task.ID, err)
			}
			return
		}
		task.State = "succeeded"
		task.LastError = ""
		if err := d.queue.Save(task); err != nil {
			d.logger.Errorf("保存任务状态失败 %s: %v", task.ID, err)
		}
		d.logger.Infof("同步任务完成: %s -> %s", task.SrcPath, task.DstPath)
		d.RemoveTask(task.ID)
	} else {
		task.Attempts++
		task.LastError = info.Error
		if task.LastError == "" {
			task.LastError = info.Status
		}
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

	// 直拷任务（无 AlistTaskID、无 RawURL）：直接 FSCopy 重提 + 状态裁决
	if task.AlistTaskID == "" && task.RawURL == "" {
		dstDir := dstDirFromPath(task.DstPath)
		srcDir := dstDirFromPath(task.SrcPath)
		srcName := fileNameFromPath(task.SrcPath)
		if err := d.client.FSCopy(ctx, srcDir, dstDir, []string{srcName}); err != nil {
			if alist.IsNotFound(err) {
				d.logger.Infof("重试源已消失，任务完成（无待同步）: %s", task.SrcPath)
				task.State = "succeeded"
				task.LastError = ""
				d.queue.Save(task)
				d.RemoveTask(task.ID)
				return
			}
			d.retryFailed(ctx, task, err)
			return
		}
		fileName := fileNameFromPath(task.DstPath)
		var wantSize int64
		if src, err := d.client.FSGetNoRetry(ctx, task.SrcPath); err == nil && src != nil {
			wantSize = src.Size
		}
		if waitCopyDoneWith(ctx, d.client, d.logger, task.SyncConfigID, task.DstPath, fileName, wantSize) {
			d.logger.Infof("重试同步任务完成: %s -> %s", task.SrcPath, task.DstPath)
			// delete_src：成功后删源；删失败不改成功结论，只告警（源残留下轮自然跳过）
			if task.DeleteSrc {
				if err := d.client.FSRemove(ctx, dstDirFromPath(task.SrcPath), []string{fileNameFromPath(task.SrcPath)}); err != nil && !alist.IsNotFound(err) {
					d.logger.Errorf("已复制但删除源 %s 失败: %v", task.SrcPath, err)
				} else {
					d.logger.Infof("已删除源: %s", task.SrcPath)
				}
			}
			task.State = "succeeded"
			task.LastError = ""
			d.queue.Save(task)
			d.RemoveTask(task.ID)
			return
		}
		d.retryFailed(ctx, task, fmt.Errorf("目标校验不通过"))
		return
	}

	// 失败任务（首次 FSPut 即失败）无 AlistTaskID，直接重新提交离线下载
	if task.AlistTaskID == "" || task.RawURL == "" {
		if task.RawURL == "" {
			task.Attempts++
			task.LastError = "缺少源直链 RawURL，无法重试"
			task.NextRetryAt = d.calcNextRetry(task.Attempts)
			d.queue.Save(task)
			return
		}
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
		task.State = "running"
		task.LastError = ""
		if err := d.queue.Save(task); err != nil {
			d.logger.Errorf("保存任务状态失败 %s: %v", task.ID, err)
		}
		return
	}

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

// retryFailed 直拷重试失败：清残留、计数、退避，超限进死信
func (d *RetryDaemon) retryFailed(ctx context.Context, task *SyncTask, err error) {
	removePartialWith(ctx, d.client, d.logger, task.DstPath)
	task.Attempts++
	task.LastError = err.Error()
	task.NextRetryAt = d.calcNextRetry(task.Attempts)
	if d.isMaxAttemptsExceeded(task) {
		task.State = "dead_letter"
		d.logger.Errorf("同步任务超过最大重试次数: %s", task.SrcPath)
	} else {
		task.State = "failed"
		d.logger.Infof("同步任务失败，将在 %v 后重试 (第 %d 次): %s",
			time.Until(task.NextRetryAt), task.Attempts, task.SrcPath)
	}
	d.queue.Save(task)
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
