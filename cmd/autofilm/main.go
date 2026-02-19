package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/extensions"
	"github.com/akimio/autofilm/internal/modules/alist2strm"
	"github.com/akimio/autofilm/internal/modules/ani2alist"
	"github.com/akimio/autofilm/internal/modules/libraryposter"
	"github.com/robfig/cron/v3"
)

var logger = core.GetLogger()

func main() {
	// 打印Logo
	extensions.PrintLogo(core.AppVersion())

	// 初始化配置
	settings := core.GetSettings()

	// 初始化日志
	core.InitLogger()
	logger = core.GetLogger()

	logger.Infof("AutoFilm %s 启动中...", core.AppVersion())
	logger.Debugf("是否开启DEBUG模式: %v", settings.IsDebug())

	// 创建上下文
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建cron调度器
	cronScheduler := cron.New(cron.WithSeconds())

	// 添加Alist2Strm任务
	if err := addAlist2StrmJobs(cronScheduler); err != nil {
		logger.Errorf("添加Alist2Strm任务失败: %v", err)
	} else {
		logger.Info("Alist2Strm任务添加完成")
	}

	// 添加Ani2Alist任务
	if err := addAni2AlistJobs(cronScheduler); err != nil {
		logger.Errorf("添加Ani2Alist任务失败: %v", err)
	} else {
		logger.Info("Ani2Alist任务添加完成")
	}

	// 添加LibraryPoster任务
	if err := addLibraryPosterJobs(cronScheduler); err != nil {
		logger.Errorf("添加LibraryPoster任务失败: %v", err)
	} else {
		logger.Info("LibraryPoster任务添加完成")
	}

	// 启动调度器
	cronScheduler.Start()
	logger.Info("AutoFilm启动完成")

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	logger.Info("接收到退出信号，正在关闭...")

	// 停止调度器
	stopCtx := cronScheduler.Stop()
	<-stopCtx.Done()

	logger.Info("AutoFilm程序退出！")
}

// addAlist2StrmJobs 添加Alist2Strm定时任务
func addAlist2StrmJobs(c *cron.Cron) error {
	settings := core.GetSettings()
	serverList := settings.GetAlistServerList()

	if len(serverList) == 0 {
		logger := core.GetLogger()
		logger.Warn("未检测到Alist2Strm模块配置")
		return nil
	}

	logger := core.GetLogger()
	logger.Info("检测到Alist2Strm模块配置，正在添加至后台任务")

	for _, server := range serverList {
		config, err := parseAlist2StrmConfig(server)
		if err != nil {
			logger.Errorf("解析Alist2Strm配置失败: %v", err)
			continue
		}

		if config.Cron == "" {
			logger.Warnf("%s 未设置cron表达式", config.ID)
			continue
		}

		_, err = c.AddFunc(config.Cron, func() {
			ctx := context.Background()
			a2s, err := alist2strm.New(config)
			if err != nil {
				logger.Errorf("创建Alist2Strm实例失败: %v", err)
				return
			}
			if err := a2s.Run(ctx); err != nil {
				logger.Errorf("Alist2Strm运行失败: %v", err)
			}
		})

		if err != nil {
			logger.Errorf("添加定时任务失败 %s: %v", config.ID, err)
		} else {
			logger.Infof("%s 已被添加至后台任务 (cron: %s)", config.ID, config.Cron)
		}
	}

	return nil
}

// addAni2AlistJobs 添加Ani2Alist定时任务
func addAni2AlistJobs(c *cron.Cron) error {
	settings := core.GetSettings()
	list := settings.GetAni2AlistList()

	if len(list) == 0 {
		logger := core.GetLogger()
		logger.Warn("未检测到Ani2Alist模块配置")
		return nil
	}

	logger := core.GetLogger()
	logger.Info("检测到Ani2Alist模块配置，正在添加至后台任务")

	for _, server := range list {
		config, err := parseAni2AlistConfig(server)
		if err != nil {
			logger.Errorf("解析Ani2Alist配置失败: %v", err)
			continue
		}

		if config.Cron == "" {
			logger.Warnf("%s 未设置cron表达式", config.ID)
			continue
		}

		_, err = c.AddFunc(config.Cron, func() {
			ctx := context.Background()
			a2a, err := ani2alist.New(config)
			if err != nil {
				logger.Errorf("创建Ani2Alist实例失败: %v", err)
				return
			}
			if err := a2a.Run(ctx); err != nil {
				logger.Errorf("Ani2Alist运行失败: %v", err)
			}
		})

		if err != nil {
			logger.Errorf("添加定时任务失败 %s: %v", config.ID, err)
		} else {
			logger.Infof("%s 已被添加至后台任务 (cron: %s)", config.ID, config.Cron)
		}
	}

	return nil
}

// addLibraryPosterJobs 添加LibraryPoster定时任务
func addLibraryPosterJobs(c *cron.Cron) error {
	settings := core.GetSettings()
	list := settings.GetLibraryPosterList()

	if len(list) == 0 {
		logger := core.GetLogger()
		logger.Warn("未检测到LibraryPoster模块配置")
		return nil
	}

	logger := core.GetLogger()
	logger.Info("检测到LibraryPoster模块配置，正在添加至后台任务")

	for _, poster := range list {
		config, err := parseLibraryPosterConfig(poster)
		if err != nil {
			logger.Errorf("解析LibraryPoster配置失败: %v", err)
			continue
		}

		if config.Cron == "" {
			logger.Warnf("%s 未设置cron表达式", config.ID)
			continue
		}

		_, err = c.AddFunc(config.Cron, func() {
			ctx := context.Background()
			lp, err := libraryposter.New(config)
			if err != nil {
				logger.Errorf("创建LibraryPoster实例失败: %v", err)
				return
			}
			if err := lp.Run(ctx); err != nil {
				logger.Errorf("LibraryPoster运行失败: %v", err)
			}
		})

		if err != nil {
			logger.Errorf("添加定时任务失败 %s: %v", config.ID, err)
		} else {
			logger.Infof("%s 已被添加至后台任务 (cron: %s)", config.ID, config.Cron)
		}
	}

	return nil
}

// parseAlist2StrmConfig 解析Alist2Strm配置
func parseAlist2StrmConfig(m map[string]interface{}) (*alist2strm.Config, error) {
	config := &alist2strm.Config{
		ID:             getString(m, "id"),
		URL:            getString(m, "url"),
		Username:       getString(m, "username"),
		Password:       getString(m, "password"),
		Token:          getString(m, "token"),
		PublicURL:      getString(m, "public_url"),
		SourceDir:      getString(m, "source_dir"),
		TargetDir:      getString(m, "target_dir"),
		FlattenMode:    getBool(m, "flatten_mode"),
		Subtitle:       getBool(m, "subtitle"),
		Image:          getBool(m, "image"),
		NFO:            getBool(m, "nfo"),
		Mode:           getString(m, "mode"),
		Overwrite:      getBool(m, "overwrite"),
		OtherExt:       getString(m, "other_ext"),
		MaxWorkers:     getInt(m, "max_workers"),
		MaxDownloaders: getInt(m, "max_downloaders"),
		WaitTime:       getFloat64(m, "wait_time"),
		SyncServer:     getBool(m, "sync_server"),
		SyncIgnore:     getString(m, "sync_ignore"),
		Cron:           getString(m, "cron"),
	}

	// 解析智能保护配置
	if sp, ok := m["smart_protection"].(map[string]interface{}); ok {
		config.SmartProtection = &alist2strm.SmartProtectionConfig{
			Enabled:    getBool(sp, "enabled"),
			Threshold:  getInt(sp, "threshold"),
			GraceScans: getInt(sp, "grace_scans"),
		}
	}

	// 设置默认值
	if config.Mode == "" {
		config.Mode = "AlistURL"
	}
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = 50
	}
	if config.MaxDownloaders <= 0 {
		config.MaxDownloaders = 5
	}

	return config, nil
}

// parseAni2AlistConfig 解析Ani2Alist配置
func parseAni2AlistConfig(m map[string]interface{}) (*ani2alist.Config, error) {
	config := &ani2alist.Config{
		ID:        getString(m, "id"),
		URL:       getString(m, "url"),
		Username:  getString(m, "username"),
		Password:  getString(m, "password"),
		Token:     getString(m, "token"),
		TargetDir: getString(m, "target_dir"),
		RSSUpdate: getBool(m, "rss_update"),
		SrcDomain: getString(m, "src_domain"),
		RSSDomain: getString(m, "rss_domain"),
		KeyWord:   getString(m, "key_word"),
		Cron:      getString(m, "cron"),
	}

	// 处理可选的年月参数
	if year, ok := m["year"].(int); ok {
		config.Year = &year
	}
	if month, ok := m["month"].(int); ok {
		config.Month = &month
	}

	return config, nil
}

// parseLibraryPosterConfig 解析LibraryPoster配置
func parseLibraryPosterConfig(m map[string]interface{}) (*libraryposter.Config, error) {
	config := &libraryposter.Config{
		ID:               getString(m, "id"),
		URL:              getString(m, "url"),
		APIKey:           getString(m, "api_key"),
		TitleFontPath:    getString(m, "title_font_path"),
		SubtitleFontPath: getString(m, "subtitle_font_path"),
		Cron:             getString(m, "cron"),
	}

	// 解析媒体库配置列表
	if cfgs, ok := m["configs"].([]interface{}); ok {
		for _, cfg := range cfgs {
			if cfgMap, ok := cfg.(map[string]interface{}); ok {
				libCfg := libraryposter.LibraryConfig{
					LibraryName: getString(cfgMap, "library_name"),
					Title:       getString(cfgMap, "title"),
					Subtitle:    getString(cfgMap, "subtitle"),
					Limit:       getInt(cfgMap, "limit"),
				}
				config.Configs = append(config.Configs, libCfg)
			}
		}
	}

	return config, nil
}

// 辅助函数：从map中获取值
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case string:
			// 尝试解析字符串
			var i int
			fmt.Sscanf(val, "%d", &i)
			return i
		}
	}
	return 0
}

func getFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		}
	}
	return 0
}
