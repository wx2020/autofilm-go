package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/akimio/autofilm/internal/core"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleGetLogs GET /api/logs?lines=500&level=warn
// 返回最近的日志行
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	linesStr := r.URL.Query().Get("lines")
	levelFilter := r.URL.Query().Get("level")

	lines := 500
	if l, err := strconv.Atoi(linesStr); err == nil && l > 0 && l <= 5000 {
		lines = l
	}

	logFile := core.CurrentLogFile()
	entries, err := tailLogFile(logFile, lines)
	if err != nil {
		if os.IsNotExist(err) {
			json.NewEncoder(w).Encode([]string{})
			return
		}
		http.Error(w, `{"error":"读取日志失败"}`, http.StatusInternalServerError)
		return
	}

	// 应用级别过滤
	if levelFilter != "" {
		var filtered []string
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry), "["+strings.ToLower(levelFilter)+"]") {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}

	json.NewEncoder(w).Encode(entries)
}

// handleLogStream WS /api/logs/stream
// 通过 WebSocket 实时推送日志
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := SubscribeLogs()
	defer UnsubscribeLogs(ch)

	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(entry)); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// LogHook WebSocket 日志钩子
type LogHook struct {
	subs   map[chan string]struct{}
	subsMu sync.RWMutex
}

var globalLogHook *LogHook

func init() {
	globalLogHook = &LogHook{
		subs: make(map[chan string]struct{}),
	}
}

// SubscribeLogs 订阅日志流
func SubscribeLogs() chan string {
	ch := make(chan string, 64)
	globalLogHook.subsMu.Lock()
	globalLogHook.subs[ch] = struct{}{}
	globalLogHook.subsMu.Unlock()
	return ch
}

// UnsubscribeLogs 取消订阅
func UnsubscribeLogs(ch chan string) {
	globalLogHook.subsMu.Lock()
	delete(globalLogHook.subs, ch)
	close(ch)
	globalLogHook.subsMu.Unlock()
}

// Fire 实现 logrus.Hook
func (h *LogHook) Fire(entry *logrus.Entry) error {
	line := fmt.Sprintf("[%s] %s | %s\n",
		strings.ToUpper(entry.Level.String()),
		entry.Time.Format("2006-01-02 15:04:05"),
		entry.Message)

	h.subsMu.RLock()
	defer h.subsMu.RUnlock()

	for ch := range h.subs {
		select {
		case ch <- line:
		default:
		}
	}
	return nil
}

// Levels 实现 logrus.Hook
func (h *LogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// tailLogFile 读取文件末尾 n 行
func tailLogFile(filePath string, n int) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
