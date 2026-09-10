package alist

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestIsNotFoundVariants(t *testing.T) {
	found := []string{
		"API错误: object not found",
		"API错误: file not found",
		"API错误: path not found",
		"API错误: no such file or directory",
		"API错误: path not exist",
		"API错误: 路径不存在",
		"API错误: 找不到对象",
		"API错误: OBJECT NOT FOUND",
	}
	for _, msg := range found {
		if !isNotFoundMessage(msg) {
			t.Errorf("应判为不存在: %q", msg)
		}
		if !IsNotFound(errString(msg)) {
			t.Errorf("IsNotFound 应为 true: %q", msg)
		}
	}

	notFound := []string{
		"API错误: EOF",
		"API错误: permission denied",
		"API错误: storage is disabled",
		"请求失败 /api/fs/list，状态码: 429",
	}
	for _, msg := range notFound {
		if isNotFoundMessage(msg) {
			t.Errorf("不应判为不存在: %q", msg)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// TestSetRateLimitMinWins 共享限流器取最小语义：低限流任务不被高限流任务带 burst
func TestSetRateLimitMinWins(t *testing.T) {
	c := &AlistClient{logger: logrus.New()}

	c.SetRateLimit(2)
	if got := c.LimitQPS(); got != 2 {
		t.Fatalf("LimitQPS() = %d, want 2", got)
	}
	c.SetRateLimit(20) // 放宽请求应被忽略
	if got := c.LimitQPS(); got != 2 {
		t.Fatalf("放宽后 LimitQPS() = %d, want 2", got)
	}
	c.SetRateLimit(1) // 更严格应生效
	if got := c.LimitQPS(); got != 1 {
		t.Fatalf("收紧后 LimitQPS() = %d, want 1", got)
	}
	c.SetRateLimit(0) // 取消保持原语义
	if got := c.LimitQPS(); got != 0 {
		t.Fatalf("取消后 LimitQPS() = %d, want 0", got)
	}
}
