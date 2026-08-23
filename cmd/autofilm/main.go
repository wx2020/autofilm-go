package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/akimio/autofilm/internal/core"
	"github.com/akimio/autofilm/internal/extensions"
	"github.com/akimio/autofilm/internal/modules/alissync"
	"github.com/akimio/autofilm/internal/modules/alist2strm"
	"github.com/akimio/autofilm/internal/modules/ani2alist"
	"github.com/akimio/autofilm/internal/modules/filemove"
	"github.com/akimio/autofilm/internal/modules/libraryposter"
	"github.com/akimio/autofilm/internal/storage"
	"github.com/akimio/autofilm/internal/web"
	"github.com/robfig/cron/v3"
)

var logger = core.GetLogger()

// 全局 alissync 守护协程列表，用于优雅关闭
var alissyncDaemons []*alissync.RetryDaemon

// 全局数据库存储
var dbStore *storage.Store

func main() {
	// 打印启动横幅
	extensions.PrintBanner(core.AppVersion())

	// 初始化配置
	settings := core.GetSettings()

	// 初始化 SQLite 数据库（P3）
	dataDir := settings.GetDataDir()
	dbCfg := storage.DefaultConfig(dataDir)
	database, err := storage.InitDB(dbCfg)
	if err != nil {
		fmt.Printf("初始化数据库失败: %v\n", err)
	} else {
		// 执行数据库迁移
		if err := storage.RunMigrations(database); err != nil {
			fmt.Printf("数据库迁移失败: %v\n", err)
		}

		// 加载/生成加密密钥
		key, err := storage.LoadOrCreateKey(dataDir)
		if err != nil {
			fmt.Printf("初始化加密密钥失败: %v\n", err)
		}

		dbStore = storage.NewStore(database, key)
		storage.SetGlobalStore(dbStore)
		if appSettings, err := dbStore.GetAppSettings(); err == nil {
			settings.ApplyRuntimeSettings(appSettings.Debug, appSettings.Timezone)
		}
		if imported, err := storage.ImportLegacyYAML(dbStore, settings.GetConfigFile()); err != nil {
			fmt.Printf("旧 YAML 配置迁移失败: %v\n", err)
		} else if imported > 0 {
			fmt.Printf("已将 %d 条旧 YAML 模块配置迁移到 SQLite\n", imported)
		}
	}

	// SQLite 设置加载完成后初始化日志，使调试模式立即生效。
	core.InitLogger()
	logger = core.GetLogger()
	logger.Infof("AutoFilm %s 启动中...", core.AppVersion())
	logger.Debugf("是否开启DEBUG模式: %v", settings.IsDebug())
	if dbStore != nil {
		logger.Info("数据库初始化完成")
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动 Web 服务
	webCfg := &web.WebConfig{
		Enabled: settings.GetWebEnabled(),
		Host:    settings.GetWebHost(),
		Port:    settings.GetWebPort(),
		Token:   settings.GetWebToken(),
	}
	if dbStore != nil {
		if appSettings, err := dbStore.GetAppSettings(); err == nil {
			webCfg.Enabled = appSettings.WebEnabled
			webCfg.Host = appSettings.WebHost
			webCfg.Port = appSettings.WebPort
			webCfg.Token = appSettings.WebToken
		}
	}
	if value, ok := os.LookupEnv("AUTOFILM_WEB_HOST"); ok {
		webCfg.Host = value
	}
	if value, ok := os.LookupEnv("AUTOFILM_WEB_TOKEN"); ok {
		webCfg.Token = value
	}
	if value, ok := os.LookupEnv("AUTOFILM_WEB_PORT"); ok {
		if port, err := strconv.Atoi(value); err == nil {
			webCfg.Port = port
		}
	}
	if value, ok := os.LookupEnv("AUTOFILM_WEB_ENABLED"); ok {
		if enabled, err := strconv.ParseBool(value); err == nil {
			webCfg.Enabled = enabled
		}
	}
	webServer := web.NewServer(webCfg)
	if err := webServer.Start(); err != nil {
		logger.Errorf("Web服务启动失败: %v", err)
	}

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

	// 添加AlistSync任务
	if err := addFileMoveJobs(cronScheduler); err != nil {
		logger.Errorf("add FileMove jobs failed: %v", err)
	}

	if err := addAlistSyncJobs(cronScheduler); err != nil {
		logger.Errorf("添加AlistSync任务失败: %v", err)
	} else {
		logger.Info("AlistSync任务添加完成")
	}

	// 启动调度器
	cronScheduler.Start()
	logger.Info("AutoFilm启动完成")

	// 监听配置重载信号（热重载）
	go func() {
		reloadCh := core.ReloadCh()
		for {
			select {
			case <-reloadCh:
				logger.Info("检测到配置变更，正在重建定时任务...")
				// 停止当前调度器
				stopCtx := cronScheduler.Stop()
				<-stopCtx.Done()

				// 清空模块注册表
				reg := web.GetModuleRegistry()
				reg.Clear()

				// 重建 cron 调度器
				cronScheduler = cron.New(cron.WithSeconds())

				addAlist2StrmJobs(cronScheduler)
				addAni2AlistJobs(cronScheduler)
				addLibraryPosterJobs(cronScheduler)
				addAlistSyncJobs(cronScheduler)
				addFileMoveJobs(cronScheduler)

				cronScheduler.Start()
				logger.Info("定时任务重建完成")
			case <-ctx.Done():
				return
			}
		}
	}()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	logger.Info("接收到退出信号，正在关闭...")

	// 停止调度器
	stopCtx := cronScheduler.Stop()
	<-stopCtx.Done()

	// 停止 alissync 守护协程
	for _, d := range alissyncDaemons {
		d.Stop()
	}

	// 关闭 Web 服务
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	webServer.Shutdown(shutdownCtx)

	logger.Info("AutoFilm程序退出！")

	// 打印退出横幅
	extensions.PrintBanner(core.AppVersion())
}

func getAlist2StrmList() []map[string]interface{} {
	return getModuleConfigs("alist2strm")
}

func getAni2AlistList() []map[string]interface{} {
	return getModuleConfigs("ani2alist")
}

func getAlisyncList() []map[string]interface{} {
	return getModuleConfigs("alissync")
}

func getFileMoveList() []map[string]interface{} {
	return getModuleConfigs("filemove")
}

func getModuleConfigs(moduleType string) []map[string]interface{} {
	if s := storage.GlobalStore(); s != nil {
		if list, err := s.ListModuleConfigs(moduleType); err == nil {
			return list
		}
	}
	return []map[string]interface{}{}
}

// addAlist2StrmJobs 添加Alist2Strm定时任务
func addAlist2StrmJobs(c *cron.Cron) error {
	serverList := getAlist2StrmList()

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

		entry := &web.ModuleEntry{
			Type:    web.ModuleAlist2Strm,
			ID:      config.ID,
			Enabled: config.Enable,
			Cron:    config.Cron,
		}
		entry.RunFunc = func() {
			err := web.TrackRun(string(web.ModuleAlist2Strm), config.ID, func() error {
				a2s, err := alist2strm.New(config)
				if err != nil {
					return err
				}
				return a2s.Run(context.Background())
			})
			if err != nil {
				logger.Errorf("Alist2Strm运行失败: %v", err)
			}
		}
		web.GetModuleRegistry().Register(entry)

		runA2S := entry.RunFunc

		_, err = c.AddFunc(config.Cron, runA2S)

		if err != nil {
			logger.Errorf("添加定时任务失败 %s: %v", config.ID, err)
		} else {
			logger.Infof("%s 已被添加至后台任务 (cron: %s)", config.ID, config.Cron)
		}

		if config.RunOnStart && config.Enable {
			logger.Infof("%s 已配置 run_on_start，启动时立即执行一次", config.ID)
			go runA2S()
		}
	}

	return nil
}

// addAni2AlistJobs 添加Ani2Alist定时任务
func addAni2AlistJobs(c *cron.Cron) error {
	list := getAni2AlistList()

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

		entry := &web.ModuleEntry{
			Type:    web.ModuleAni2Alist,
			ID:      config.ID,
			Enabled: config.Enable,
			Cron:    config.Cron,
		}
		entry.RunFunc = func() {
			err := web.TrackRun(string(web.ModuleAni2Alist), config.ID, func() error {
				a2a, err := ani2alist.New(config)
				if err != nil {
					return err
				}
				return a2a.Run(context.Background())
			})
			if err != nil {
				logger.Errorf("Ani2Alist运行失败: %v", err)
			}
		}
		web.GetModuleRegistry().Register(entry)

		runA2A := entry.RunFunc

		_, err = c.AddFunc(config.Cron, runA2A)

		if err != nil {
			logger.Errorf("添加定时任务失败 %s: %v", config.ID, err)
		} else {
			logger.Infof("%s 已被添加至后台任务 (cron: %s)", config.ID, config.Cron)
		}

		if config.RunOnStart && config.Enable {
			logger.Infof("%s 已配置 run_on_start，启动时立即执行一次", config.ID)
			go runA2A()
		}
	}

	return nil
}

// addLibraryPosterJobs 添加LibraryPoster定时任务
func addLibraryPosterJobs(c *cron.Cron) error {
	list := getModuleConfigs("libraryposter")

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

		entry := &web.ModuleEntry{
			Type:    web.ModuleLibraryPoster,
			ID:      config.ID,
			Enabled: config.Enable,
			Cron:    config.Cron,
		}
		entry.RunFunc = func() {
			err := web.TrackRun(string(web.ModuleLibraryPoster), config.ID, func() error {
				lp, err := libraryposter.New(config)
				if err != nil {
					return err
				}
				return lp.Run(context.Background())
			})
			if err != nil {
				logger.Errorf("LibraryPoster运行失败: %v", err)
			}
		}
		web.GetModuleRegistry().Register(entry)

		runLP := entry.RunFunc

		_, err = c.AddFunc(config.Cron, runLP)

		if err != nil {
			logger.Errorf("添加定时任务失败 %s: %v", config.ID, err)
		} else {
			logger.Infof("%s 已被添加至后台任务 (cron: %s)", config.ID, config.Cron)
		}

		if config.RunOnStart && config.Enable {
			logger.Infof("%s 已配置 run_on_start，启动时立即执行一次", config.ID)
			go runLP()
		}
	}

	return nil
}

// addAlistSyncJobs 添加AlistSync定时任务
func addAlistSyncJobs(c *cron.Cron) error {
	list := getAlisyncList()

	if len(list) == 0 {
		logger := core.GetLogger()
		logger.Warn("未检测到AlistSync模块配置")
		return nil
	}

	logger := core.GetLogger()
	logger.Info("检测到AlistSync模块配置，正在添加至后台任务")

	for _, s := range list {
		config, err := parseAlistSyncConfig(s)
		if err != nil {
			logger.Errorf("解析AlistSync配置失败: %v", err)
			continue
		}

		if config.Cron == "" {
			logger.Warnf("AlistSync %s 未设置cron表达式", config.ID)
			continue
		}

		entry := &web.ModuleEntry{
			Type:    web.ModuleAlissync,
			ID:      config.ID,
			Enabled: config.Enable,
			Cron:    config.Cron,
		}
		entry.RunFunc = func() {
			err := web.TrackRun(string(web.ModuleAlissync), config.ID, func() error {
				syncer, err := alissync.New(config)
				if err != nil {
					return err
				}
				alissyncDaemons = append(alissyncDaemons, syncer.Daemon())
				return syncer.Run(context.Background())
			})
			if err != nil {
				logger.Errorf("AlistSync运行失败: %v", err)
			}
		}
		web.GetModuleRegistry().Register(entry)

		runSync := entry.RunFunc

		_, err = c.AddFunc(config.Cron, runSync)

		if err != nil {
			logger.Errorf("添加定时任务失败 %s: %v", config.ID, err)
		} else {
			logger.Infof("AlistSync %s 已被添加至后台任务 (cron: %s)", config.ID, config.Cron)
		}

		if config.RunOnStart && config.Enable {
			logger.Infof("AlistSync %s 已配置 run_on_start，启动时立即执行一次", config.ID)
			go runSync()
		}
	}

	return nil
}

// parseAlistSyncConfig 解析AlistSync配置
func addFileMoveJobs(c *cron.Cron) error {
	list := getFileMoveList()
	if len(list) == 0 {
		return nil
	}

	logger := core.GetLogger()
	for _, raw := range list {
		config, err := parseFileMoveConfig(raw)
		if err != nil {
			logger.Errorf("parse FileMove config failed: %v", err)
			continue
		}
		if config.Cron == "" {
			logger.Warnf("FileMove %s has no cron expression", config.ID)
			continue
		}

		entry := &web.ModuleEntry{
			Type:    web.ModuleFileMove,
			ID:      config.ID,
			Enabled: config.Enable,
			Cron:    config.Cron,
		}
		entry.RunFunc = func() {
			if !entry.Enabled {
				return
			}
			err := web.TrackRun(string(web.ModuleFileMove), config.ID, func() error {
				mover, err := filemove.New(config)
				if err != nil {
					return err
				}
				report, err := mover.Move(context.Background())
				logger.Infof("FileMove %s completed: scanned=%d matched=%d renamed=%d moved=%d skipped=%d errors=%d",
					config.ID, report.Scanned, report.Matched, report.Renamed, report.Moved, report.Skipped, len(report.Errors))
				return err
			})
			if err != nil {
				logger.Errorf("FileMove run failed: %v", err)
			}
		}
		web.GetModuleRegistry().Register(entry)
		runFileMove := entry.RunFunc
		if _, err := c.AddFunc(config.Cron, runFileMove); err != nil {
			logger.Errorf("add FileMove job failed %s: %v", config.ID, err)
		}
		if config.RunOnStart && config.Enable {
			go runFileMove()
		}
	}
	return nil
}

func parseFileMoveConfig(m map[string]interface{}) (*filemove.Config, error) {
	config := &filemove.Config{
		ID:                getString(m, "id"),
		Enable:            getEnable(m, "enable"),
		RunOnStart:        getBool(m, "run_on_start"),
		SourceDir:         getString(m, "source_dir"),
		TargetDir:         getString(m, "target_dir"),
		Regex:             getString(m, "regex"),
		MinSize:           0,
		MaxSize:           0,
		Overwrite:         getBool(m, "overwrite"),
		Flatten:           getBool(m, "flatten"),
		RenameRegex:       getString(m, "rename_regex"),
		RenameReplacement: getString(m, "rename_replacement"),
		Cron:              getString(m, "cron"),
		Backend:           getString(m, "backend"),
		URL:               getString(m, "url"),
		Username:          getString(m, "username"),
		Password:          getString(m, "password"),
		Token:             getString(m, "token"),
	}
	if value, ok := m["size"]; ok && value != nil && strings.TrimSpace(getString(m, "size")) != "" {
		size, err := filemove.ParseSize(value)
		if err != nil {
			return nil, fmt.Errorf("parse filemove size: %w", err)
		}
		config.Size = &size
	}
	for key, target := range map[string]*int64{"min_size": &config.MinSize, "max_size": &config.MaxSize} {
		if value, ok := m[key]; ok && value != nil && strings.TrimSpace(getString(m, key)) != "" {
			size, err := filemove.ParseSize(value)
			if err != nil {
				return nil, fmt.Errorf("parse filemove %s: %w", key, err)
			}
			*target = size
		}
	}
	return config, nil
}

func parseAlistSyncConfig(m map[string]interface{}) (*alissync.Config, error) {
	config := &alissync.Config{
		ID:         getString(m, "id"),
		Enable:     getEnable(m, "enable"),
		RunOnStart: getBool(m, "run_on_start"),
		URL:        getString(m, "url"),
		Username:   getString(m, "username"),
		Password:   getString(m, "password"),
		Token:      getString(m, "token"),
		WaitTime:   getFloat64(m, "wait_time"),
		Cron:       getString(m, "cron"),
	}

	// 解析 pairs
	if pairsRaw, ok := m["pairs"].([]interface{}); ok {
		for _, p := range pairsRaw {
			if pairMap, ok := p.(map[string]interface{}); ok {
				pair := alissync.PairConfig{
					Src:       getString(pairMap, "src"),
					Dst:       getString(pairMap, "dst"),
					DeleteSrc: getBool(pairMap, "delete_src"),
					Overwrite: getString(pairMap, "overwrite"),
				}
				config.Pairs = append(config.Pairs, pair)
			}
		}
	}

	// 解析 retry
	if retryRaw, ok := m["retry"].(map[string]interface{}); ok {
		config.Retry = alissync.RetryConfig{
			MaxAttempts: getInt(retryRaw, "max_attempts"),
			Backoff:     getString(retryRaw, "backoff"),
			Jitter:      getFloat64(retryRaw, "jitter"),
		}
	}

	// 默认值
	if config.Retry.MaxAttempts <= 0 {
		config.Retry.MaxAttempts = 10
	}
	if config.Retry.Backoff == "" {
		config.Retry.Backoff = "expo"
	}

	return config, nil
}

// parseAlist2StrmConfig 解析Alist2Strm配置
func parseAlist2StrmConfig(m map[string]interface{}) (*alist2strm.Config, error) {
	config := &alist2strm.Config{
		ID:             getString(m, "id"),
		Enable:         getEnable(m, "enable"),
		RunOnStart:     getBool(m, "run_on_start"),
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
		ScanMode:       getString(m, "scan_mode"),
		QPSLimit:       getInt(m, "qps_limit"),
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
		ID:         getString(m, "id"),
		Enable:     getEnable(m, "enable"),
		RunOnStart: getBool(m, "run_on_start"),
		URL:        getString(m, "url"),
		Username:   getString(m, "username"),
		Password:   getString(m, "password"),
		Token:      getString(m, "token"),
		TargetDir:  getString(m, "target_dir"),
		RSSUpdate:  getBool(m, "rss_update"),
		SrcDomain:  getString(m, "src_domain"),
		RSSDomain:  getString(m, "rss_domain"),
		KeyWord:    getString(m, "key_word"),
		Cron:       getString(m, "cron"),
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
		Enable:           getEnable(m, "enable"),
		RunOnStart:       getBool(m, "run_on_start"),
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
		switch val := v.(type) {
		case string:
			return val
		case int:
			return fmt.Sprintf("%d", val)
		case int64:
			return fmt.Sprintf("%d", val)
		case float64:
			// 整数值的 float64 也转成字符串，避免 yaml 把 115 解析为 float 又丢失
			return fmt.Sprintf("%g", val)
		case bool:
			if val {
				return "true"
			}
			return "false"
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

// getEnable 读取启动开关字段，缺省视为启用
func getEnable(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
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

func getInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return int64(val)
		case int8:
			return int64(val)
		case int16:
			return int64(val)
		case int32:
			return int64(val)
		case int64:
			return val
		case uint:
			return int64(val)
		case uint64:
			return int64(val)
		case float64:
			return int64(val)
		case string:
			var n int64
			fmt.Sscanf(val, "%d", &n)
			return n
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
