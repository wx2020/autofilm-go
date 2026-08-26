package alistsync

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/pkg/alist"
	"github.com/sirupsen/logrus"
)

// PairConfig 同步对配置
type PairConfig struct {
	Src       string `yaml:"src"`
	Dst       string `yaml:"dst"`
	DeleteSrc bool   `yaml:"delete_src"`
	Overwrite string `yaml:"overwrite"` // never, always, if_newer
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts int     `yaml:"max_attempts"`
	Backoff     string  `yaml:"backoff"` // expo
	Jitter      float64 `yaml:"jitter"`
}

// Config Alissync 配置
type Config struct {
	ID         string
	Enable     bool
	RunOnStart bool
	URL        string
	Username   string
	Password   string
	Token      string
	Pairs      []PairConfig
	Retry      RetryConfig
	WaitTime   float64
	QPSLimit   int // 对 OpenList API 的限流（次/秒），0 表示不限流
	Cron       string
}

// Alissync Alist 同步器
type Alissync struct {
	config *Config
	client *alist.AlistClient
	queue  *QueueManager
	daemon *RetryDaemon
	logger *logrus.Logger
}

// New 创建 Alissync 实例
func New(cfg *Config) (*Alissync, error) {
	client, err := alist.GetClient(cfg.URL, cfg.Username, cfg.Password, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("创建Alist客户端失败: %w", err)
	}

	// 声明本任务的限流策略（0 表示不限流）。
	// 共享同一服务器+凭据的多个任务共用一个限流器，后启动者的配置生效。
	client.SetRateLimit(cfg.QPSLimit)

	settings := core.GetSettings()
	syncQueueDir := filepath.Join(settings.GetConfigDir(), "sync_queue")
	queue := NewQueueManager(syncQueueDir, core.GetLogger())

	if cfg.Retry.MaxAttempts <= 0 {
		cfg.Retry.MaxAttempts = 10
	}
	if cfg.Retry.Backoff == "" {
		cfg.Retry.Backoff = "expo"
	}

	as := &Alissync{
		config: cfg,
		client: client,
		queue:  queue,
		logger: core.GetLogger(),
	}

	as.daemon = NewRetryDaemon(client, queue, &cfg.Retry, as.logger)

	return as, nil
}

// Run 执行同步
func (as *Alissync) Run(ctx context.Context) error {
	as.logger.Infof("开始 Alissync 同步: %s", as.config.ID)

	// 确保守护协程已启动
	as.daemon.Start(ctx)

	// 同步每个 pair
	for _, pair := range as.config.Pairs {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := as.syncPair(ctx, pair); err != nil {
			as.logger.Errorf("同步 pair 失败 %s -> %s: %v", pair.Src, pair.Dst, err)
		}
	}

	as.logger.Infof("Alissync 同步完成: %s", as.config.ID)
	return nil
}

// Daemon 返回守护协程（供外部调用 Start/Stop）
func (as *Alissync) Daemon() *RetryDaemon {
	return as.daemon
}
