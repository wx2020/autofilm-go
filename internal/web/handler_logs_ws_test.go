package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// TestLogStreamHandshake 回归：requestLogger 的包装 writer 必须透传 Hijacker，
// 否则 gorilla 无法升级连接，/api/logs/stream 恒返回 500。
func TestLogStreamHandshake(t *testing.T) {
	handler := NewServer(&WebConfig{Enabled: true, Host: "127.0.0.1", Port: 8080, Token: "secret"}).httpServer.Handler
	srv := httptest.NewServer(handler)
	defer srv.Close()
	base := "ws" + srv.URL[4:] + "/api/logs/stream"

	// 坏 token → 401
	_, resp, err := websocket.DefaultDialer.Dial(base+"?token=wrong", nil)
	if err == nil {
		t.Fatal("坏 token 不应握手成功")
	}
	if resp == nil {
		t.Fatalf("无响应: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("坏 token 握手状态码 = %d，want 401", resp.StatusCode)
	}

	// 有效 token → 101，且能收到订阅后产生的一条日志
	conn, _, err := websocket.DefaultDialer.Dial(base+"?token=secret", nil)
	if err != nil {
		t.Fatalf("有效 token 握手失败: %v", err)
	}
	defer conn.Close()

	entry := logrus.NewEntry(logrus.New())
	entry.Message = "ws-hijack-probe"
	entry.Level = logrus.InfoLevel
	entry.Time = time.Now()
	if err := GetLogHook().Fire(entry); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("读取推送消息失败: %v", err)
	}
	if len(msg) == 0 {
		t.Fatal("推送消息为空")
	}
}
