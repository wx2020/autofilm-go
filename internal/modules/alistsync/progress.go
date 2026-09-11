package alistsync

import (
	"sync"
	"time"
)

// CopyProgress 正在复制的文件进度（内存态，供前端进度条轮询）
type CopyProgress struct {
	ConfigID  string    `json:"config_id"`
	File      string    `json:"file"`
	Progress  float64   `json:"progress"`
	UpdatedAt time.Time `json:"updated_at"`
}

var (
	progressMu  sync.Mutex
	progressMap = map[string]*CopyProgress{}
	// progressTTL 停更超时的进度视为过期（任务崩溃未清理时兜底）
	progressTTL = 60 * time.Second
)

// progressKey 进度键：多 worker 下同一配置可同时拷多个文件
func progressKey(configID, file string) string {
	return configID + "\x00" + file
}

// SetCopyProgress 上报进度
func SetCopyProgress(configID, file string, progress float64) {
	progressMu.Lock()
	defer progressMu.Unlock()
	progressMap[progressKey(configID, file)] = &CopyProgress{
		ConfigID:  configID,
		File:      file,
		Progress:  progress,
		UpdatedAt: time.Now(),
	}
}

// ClearCopyProgress 清除进度（文件完成/失败/跳过时调用）
func ClearCopyProgress(configID, file string) {
	progressMu.Lock()
	defer progressMu.Unlock()
	delete(progressMap, progressKey(configID, file))
}

// ListCopyProgress 列出未过期的实时进度
func ListCopyProgress() []CopyProgress {
	progressMu.Lock()
	defer progressMu.Unlock()
	now := time.Now()
	out := make([]CopyProgress, 0, len(progressMap))
	for id, p := range progressMap {
		if now.Sub(p.UpdatedAt) > progressTTL {
			delete(progressMap, id)
			continue
		}
		out = append(out, *p)
	}
	return out
}
