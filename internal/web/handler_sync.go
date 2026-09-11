package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/akimio/autofilm/internal/modules/alistsync"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleGetSyncQueue(w http.ResponseWriter, _ *http.Request) {
	store := storage.GlobalStore()
	if store == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "数据库不可用")
		return
	}
	tasks, err := store.ListAllSyncTasks()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleRetrySyncTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "任务 ID 无效")
		return
	}
	store := storage.GlobalStore()
	task, err := store.GetSyncTaskByID(id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "任务未找到")
		return
	}
	task.State = "failed"
	task.LastError = "手动触发重试"
	now := time.Now()
	task.NextRetryAt = &now
	if err := store.UpdateSyncTask(task); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleDeleteSyncTask DELETE /api/sync/queue/{tid} 删除同步队列任务。
// 运行中任务不允许删（先等它跑完）；删除同步清守护协程内存表，防止下轮 poll 写活。
func (s *Server) handleDeleteSyncTask(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "tid"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "任务 ID 无效")
		return
	}
	store := storage.GlobalStore()
	task, err := store.GetSyncTaskByID(id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "任务未找到")
		return
	}
	if task.State == "running" {
		writeJSONError(w, http.StatusConflict, "任务运行中，请稍后再删")
		return
	}
	if err := store.DeleteSyncTaskByDstPath(task.DstPath); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	alistsync.DropTaskFromDaemons(task.DstPath)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleTaskRuns(w http.ResponseWriter, _ *http.Request) {
	runs, err := storage.GlobalStore().ListRecentTaskRuns(200)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, runs)
}
