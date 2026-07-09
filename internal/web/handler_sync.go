package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/akimio/autofilm/internal/core"
	"github.com/go-chi/chi/v5"
)

// handleGetSyncQueue GET /api/sync/queue
func (s *Server) handleGetSyncQueue(w http.ResponseWriter, r *http.Request) {
	queueDir := filepath.Join(core.Store().GetConfigDir(), "sync_queue")
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			json.NewEncoder(w).Encode([]interface{}{})
			return
		}
		http.Error(w, `{"error":"读取队列失败"}`, http.StatusInternalServerError)
		return
	}

	var tasks []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(queueDir, entry.Name()))
		if err != nil {
			continue
		}
		var task map[string]interface{}
		if err := json.Unmarshal(data, &task); err != nil {
			continue
		}
		tasks = append(tasks, task)
	}

	json.NewEncoder(w).Encode(tasks)
}

// handleRetrySyncTask POST /api/sync/queue/retry/{tid}
func (s *Server) handleRetrySyncTask(w http.ResponseWriter, r *http.Request) {
	tid := chi.URLParam(r, "tid")
	if tid == "" {
		http.Error(w, `{"error":"缺少任务ID"}`, http.StatusBadRequest)
		return
	}

	// 从队列目录读取任务 JSON，将 state 改为 "failed" 以便守护协程重试
	queueDir := filepath.Join(core.Store().GetConfigDir(), "sync_queue")
	taskPath := filepath.Join(queueDir, tid+".json")

	data, err := os.ReadFile(taskPath)
	if err != nil {
		http.Error(w, `{"error":"任务未找到"}`, http.StatusNotFound)
		return
	}

	var task map[string]interface{}
	if err := json.Unmarshal(data, &task); err != nil {
		http.Error(w, `{"error":"解析任务失败"}`, http.StatusInternalServerError)
		return
	}

	task["state"] = "failed"
	task["last_error"] = "手动触发重试"

	// 原子写入
	tmpPath := taskPath + ".tmp"
	updated, _ := json.MarshalIndent(task, "", "  ")
	os.WriteFile(tmpPath, updated, 0644)
	os.Rename(tmpPath, taskPath)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "任务已加入重试队列",
	})
}
