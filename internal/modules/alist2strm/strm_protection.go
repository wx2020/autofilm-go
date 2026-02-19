package alist2strm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/sirupsen/logrus"
)

// StrmProtectionManager STRM文件保护管理器
type StrmProtectionManager struct {
	targetDir   string
	taskID      string
	threshold   int
	graceScans  int
	stateFile   string
	protected   map[string]int
	mu          sync.RWMutex
	logger      *logrus.Logger
}

// ProtectionState 保护状态文件内容
type ProtectionState struct {
	Updated   string           `json:"updated"`
	Protected map[string]int   `json:"protected"`
}

// NewStrmProtectionManager 创建STRM保护管理器
func NewStrmProtectionManager(targetDir, taskID string, threshold, graceScans int) *StrmProtectionManager {
	return &StrmProtectionManager{
		targetDir:  targetDir,
		taskID:     taskID,
		threshold:  threshold,
		graceScans: graceScans,
		stateFile:  filepath.Join(targetDir, ".autofilm_strm_"+taskID+".json"),
		protected:  make(map[string]int),
		logger:     core.GetLogger(),
	}
}

// Load 加载保护状态
func (spm *StrmProtectionManager) Load() error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	data, err := os.ReadFile(spm.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在是正常情况
		}
		return err
	}

	var state ProtectionState
	if err := json.Unmarshal(data, &state); err != nil {
		spm.logger.Warnf("加载保护状态失败: %v，重新开始", err)
		return nil
	}

	spm.protected = state.Protected
	return nil
}

// Save 保存保护状态
func (spm *StrmProtectionManager) Save() error {
	spm.mu.RLock()
	defer spm.mu.RUnlock()

	// 原子写入
	tmpFile := spm.stateFile + ".tmp"
	state := ProtectionState{
		Updated:   time.Now().Format(time.RFC3339),
		Protected: spm.protected,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	// 原子替换
	return os.Rename(tmpFile, spm.stateFile)
}

// Process 处理待删除的STRM文件
func (spm *StrmProtectionManager) Process(strmToDelete, strmPresent map[string]struct{}) map[string]struct{} {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	// 检查已恢复的文件
	returned := 0
	for relPath := range spm.protected {
		if _, exists := strmPresent[relPath]; exists {
			delete(spm.protected, relPath)
			returned++
		}
	}

	if returned > 0 {
		spm.logger.Infof("%d 个.strm文件已恢复，取消保护", returned)
	}

	if len(strmToDelete) < spm.threshold {
		if len(strmToDelete) > 0 {
			spm.logger.Infof("正常删除 %d 个.strm（阈值：%d）", len(strmToDelete), spm.threshold)
		}
		return strmToDelete
	}

	spm.logger.Warnf("保护激活：%d 个.strm待删除（阈值：%d）", len(strmToDelete), spm.threshold)

	// 增加保护计数
	for filePath := range strmToDelete {
		relPath := toRelativePath(spm.targetDir, filePath)
		spm.protected[relPath] = spm.protected[relPath] + 1
	}

	// 找出需要删除的文件（达到宽限期扫描次数）
	readyRel := make(map[string]struct{})
	for relPath, count := range spm.protected {
		if count >= spm.graceScans {
			readyRel[relPath] = struct{}{}
			delete(spm.protected, relPath)
		}
	}

	pending := len(spm.protected)

	if len(readyRel) > 0 {
		spm.logger.Warnf("删除 %d 个.strm（经过 %d 次扫描确认）", len(readyRel), spm.graceScans)
	}

	if pending > 0 {
		spm.logger.Infof("%d 个文件等待确认", pending)
	}

	// 转换回绝对路径
	result := make(map[string]struct{})
	for relPath := range readyRel {
		result[toAbsolutePath(spm.targetDir, relPath)] = struct{}{}
	}

	return result
}

func toRelativePath(baseDir, filePath string) string {
	relPath, err := filepath.Rel(baseDir, filePath)
	if err != nil {
		return filePath
	}
	return relPath
}

func toAbsolutePath(baseDir, relPath string) string {
	return filepath.Join(baseDir, relPath)
}
