package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akimio/autofilm/internal/storage"
)

type metricState struct {
	StartedAt time.Time         `json:"started_at"`
	Runs      map[string]uint64 `json:"runs"`
	Failures  map[string]uint64 `json:"failures"`
	Running   int64             `json:"running"`
	mu        sync.RWMutex
}

func handlePrometheusMetrics(w http.ResponseWriter, _ *http.Request) {
	snapshot := metricsSnapshot()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP autofilm_uptime_seconds Process uptime.\n# TYPE autofilm_uptime_seconds gauge\nautofilm_uptime_seconds %v\n", snapshot["uptime_seconds"])
	fmt.Fprintf(w, "# HELP autofilm_tasks_running Currently running module tasks.\n# TYPE autofilm_tasks_running gauge\nautofilm_tasks_running %v\n", snapshot["running"])
	metricsState.mu.RLock()
	defer metricsState.mu.RUnlock()
	fmt.Fprintln(w, "# HELP autofilm_module_runs_total Module task executions.\n# TYPE autofilm_module_runs_total counter")
	for moduleType, value := range metricsState.Runs {
		fmt.Fprintf(w, "autofilm_module_runs_total{module=%q} %d\n", moduleType, value)
	}
	fmt.Fprintln(w, "# HELP autofilm_module_failures_total Failed module executions.\n# TYPE autofilm_module_failures_total counter")
	for moduleType, value := range metricsState.Failures {
		fmt.Fprintf(w, "autofilm_module_failures_total{module=%q} %d\n", moduleType, value)
	}
}

func TrackRun(moduleType, configID string, fn func() error) error {
	recordRunStart(moduleType)
	var runID int64
	if store := storage.GlobalStore(); store != nil {
		runID, _ = store.CreateTaskRun(&storage.TaskRun{
			ModuleType: moduleType, ConfigUID: configID, StartedAt: time.Now(), Status: "running",
		})
	}
	err := fn()
	recordRunFinish(moduleType, err != nil)
	if store := storage.GlobalStore(); store != nil {
		status, summary := "success", ""
		if err != nil {
			status, summary = "failed", err.Error()
			_ = store.CreateAlert("error", moduleType+":"+configID, summary)
			if settings, settingErr := store.GetAppSettings(); settingErr == nil && settings.AlertWebhook != "" {
				go sendAlertWebhook(settings.AlertWebhook, moduleType+":"+configID, summary)
			}
		}
		if runID > 0 {
			_ = store.FinishTaskRun(runID, status, summary, 0, 0, 0, 0)
		}
	}
	return err
}

func sendAlertWebhook(url, source, message string) {
	payload, _ := json.Marshal(map[string]string{"level": "error", "source": source, "message": message})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err == nil {
		_ = resp.Body.Close()
	}
}

var metricsState = metricState{StartedAt: time.Now(), Runs: map[string]uint64{}, Failures: map[string]uint64{}}

func recordRunStart(moduleType string) {
	metricsState.mu.Lock()
	metricsState.Runs[moduleType]++
	metricsState.mu.Unlock()
	atomic.AddInt64(&metricsState.Running, 1)
}

func recordRunFinish(moduleType string, failed bool) {
	atomic.AddInt64(&metricsState.Running, -1)
	if failed {
		metricsState.mu.Lock()
		metricsState.Failures[moduleType]++
		metricsState.mu.Unlock()
	}
}

func metricsSnapshot() map[string]interface{} {
	metricsState.mu.RLock()
	defer metricsState.mu.RUnlock()
	runs := map[string]uint64{}
	failures := map[string]uint64{}
	for k, v := range metricsState.Runs {
		runs[k] = v
	}
	for k, v := range metricsState.Failures {
		failures[k] = v
	}
	return map[string]interface{}{
		"started_at":     metricsState.StartedAt,
		"uptime_seconds": int64(time.Since(metricsState.StartedAt).Seconds()),
		"running":        atomic.LoadInt64(&metricsState.Running),
		"runs":           runs,
		"failures":       failures,
	}
}
