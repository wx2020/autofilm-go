package alistsync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
	"github.com/sirupsen/logrus"
)

// 注意 /api/fs/copy 是异步接口：code 200 仅表示已提交（服务端日志里
// POST 200 仅几百微秒，真拷跑在后台 copy 任务里；同盘快拷才显得像同步），
// 不能只等状态码，必须看服务端状态裁决：undone/done/info 三接口 + 目标校验。

// copyPollInterval 状态轮询间隔
const copyPollInterval = 5 * time.Second

// copyMissLimit 已绑定任务连续消失上限：任务出现过、随后在 undone/done
// 两表都找不到且目标未就绪，累计达限即判任务丢失（记错下轮重试）。
// 这是“状态缺席计数”不是墙钟超时：从未出现过任务只管等（同盘快拷
// 可能根本不建任务）；退出只看三接口状态与 ctx 取消。
const copyMissLimit = 12

// syncPair 同步一对源目目录（同实例服务端直拷，不走离线下载）
// 语义：按覆盖策略逐文件 FSCopy；完成与否看目标状态裁决。
func (as *Alissync) syncPair(ctx context.Context, pair PairConfig) error {
	as.logger.Infof("开始同步: %s -> %s", pair.Src, pair.Dst)

	waitTime := time.Duration(as.config.WaitTime) * time.Second

	// 递归列出源目录所有文件
	srcFiles, err := as.listRecursive(ctx, pair.Src, waitTime)
	if err != nil {
		return err
	}

	as.logger.Infof("源目录 %s 共 %d 个文件", pair.Src, len(srcFiles))

	// 按覆盖策略过滤出需要复制的文件
	var toSync []alist.AlistPath
	for _, f := range srcFiles {
		dstPath := replacePrefix(f.FullPath, pair.Src, pair.Dst)

		// 检查目标是否存在（不存在为正常预期，不打 ERROR）
		existing, err := as.client.FSGet(ctx, dstPath)
		if err != nil && !alist.IsNotFound(err) {
			as.logger.Warnf("检查目标状态失败 %s: %v（按需同步继续）", dstPath, err)
			existing = nil
		}

		// 应用覆盖策略
		if !ShouldOverwrite(OverwritePolicy(pair.Overwrite), &f, existing) {
			as.logger.Debugf("跳过已同步文件: %s", f.FullPath)
			continue
		}

		toSync = append(toSync, f)
	}

	if len(toSync) == 0 {
		as.logger.Infof("目录 %s 无文件需要同步", pair.Src)
		return nil
	}

	as.logger.Infof("需要同步 %d 个文件", len(toSync))

	// 创建目标目录结构
	dirs := collectDirs(toSync, pair.Src, pair.Dst)
	for _, dir := range dirs {
		if err := as.client.FSMkdir(ctx, dir); err != nil {
			as.logger.Warnf("创建目录失败 %s: %v", dir, err)
		}
	}

	// worker 数跟随后端 copy_task_threads_num：开几个配额就跑几个并发，
	// 背压由服务端说了算；失败任务入守护队列按退避重试，不在本轮死磕。
	workers := as.resolveWorkers(ctx)
	jobs := make(chan alist.AlistPath, workers*2)
	var mu sync.Mutex
	var copied, skipped int
	var errs []error

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if ctx.Err() != nil {
					return
				}
				ok, skip, failErr := as.copyOne(ctx, pair, f)
				mu.Lock()
				switch {
				case ok:
					copied++
					// 复制成功但删源失败等附带错误：照样记错告警，不丢
					if failErr != nil {
						errs = append(errs, failErr)
					}
				case skip:
					skipped++
				default:
					errs = append(errs, failErr)
				}
				mu.Unlock()
			}
		}()
	}
dispatch:
	for _, f := range toSync {
		select {
		case jobs <- f:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	as.logger.Infof("同步完成: %s -> %s，复制=%d 跳过=%d 失败=%d", pair.Src, pair.Dst, copied, skipped, len(errs))
	if len(errs) > 0 {
		msgs := make([]string, 0, len(errs))
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		return fmt.Errorf("%d 个文件复制失败: %s", len(errs), strings.Join(msgs, "; "))
	}
	return nil
}

// copyOne 单文件复制（单次提交 + 状态裁决），返回 (成功, 跳过, 失败错误)。
// 源消失记跳过；失败不重试——由调用方入守护队列按退避重试。
func (as *Alissync) copyOne(ctx context.Context, pair PairConfig, f alist.AlistPath) (bool, bool, error) {
	dstPath := replacePrefix(f.FullPath, pair.Src, pair.Dst)
	srcDir := dstDirFromPath(f.FullPath)
	srcName := fileNameFromPath(f.FullPath)
	dstDir := dstDirFromPath(dstPath)
	dstName := fileNameFromPath(dstPath)
	fileName := fileNameFromPath(f.FullPath)

	// 前端进度条：提交即 0%，完成/失败/跳过清掉（defer 兜底崩溃残留由 TTL 清理）
	SetCopyProgress(as.config.ID, fileName, 0)
	defer ClearCopyProgress(as.config.ID, fileName)

	// 目标已存在且策略允许覆盖时，先删旧目标，避免服务端撞名自动改名
	if existing, err := as.client.FSGetNoRetry(ctx, dstPath); err == nil && existing != nil {
		if err := as.client.FSRemove(ctx, dstDir, []string{dstName}); err != nil {
			return false, false, fmt.Errorf("删除已存在的目标 %s 失败: %w", dstPath, err)
		}
	}

	if err := as.client.FSCopy(ctx, srcDir, dstDir, []string{srcName}); err != nil {
		if alist.IsNotFound(err) {
			as.logger.Warnf("源文件已消失，跳过: %s", f.FullPath)
			return false, true, nil
		}
		// 歧义失败：提交可能已受理，看目标裁决
		if as.waitCopyDone(ctx, dstPath, fileName, f.Size) {
			as.logger.Infof("已复制（状态裁决）: %s -> %s", f.FullPath, dstPath)
			return as.afterCopied(ctx, pair, f.FullPath, dstPath)
		}
		as.enqueueFailed(ctx, pair, f.FullPath, dstPath, err)
		return false, false, fmt.Errorf("复制 %s -> %s 失败（已入重试队列）: %w", f.FullPath, dstPath, err)
	}
	// code 200 仅表示已提交：看目标裁决
	if as.waitCopyDone(ctx, dstPath, fileName, f.Size) {
		as.logger.Infof("已复制: %s -> %s", f.FullPath, dstPath)
		return as.afterCopied(ctx, pair, f.FullPath, dstPath)
	}
	as.enqueueFailed(ctx, pair, f.FullPath, dstPath, fmt.Errorf("目标校验不通过"))
	return false, false, fmt.Errorf("复制 %s -> %s 未完成（已入重试队列）", f.FullPath, dstPath)
}

// afterCopied 复制成功后收尾：delete_src 开启则删源。
// 删源失败记错（源残留，下轮按覆盖策略自然跳过，不丢数据）。
func (as *Alissync) afterCopied(ctx context.Context, pair PairConfig, srcFullPath, dstPath string) (bool, bool, error) {
	if !pair.DeleteSrc {
		return true, false, nil
	}
	if err := as.client.FSRemove(ctx, dstDirFromPath(srcFullPath), []string{fileNameFromPath(srcFullPath)}); err != nil {
		if alist.IsNotFound(err) {
			return true, false, nil
		}
		return true, false, fmt.Errorf("已复制但删除源 %s 失败: %w", srcFullPath, err)
	}
	as.logger.Infof("已删除源: %s", srcFullPath)
	return true, false, nil
}

// enqueueFailed 失败任务入守护队列，按指数退避重试
func (as *Alissync) enqueueFailed(ctx context.Context, pair PairConfig, srcFullPath, dstPath string, err error) {
	as.removePartial(ctx, dstPath)
	task := &SyncTask{
		ID:           dstPath,
		SyncConfigID: as.config.ID,
		SrcPath:      srcFullPath,
		DstPath:      dstPath,
		State:        "failed",
		Attempts:     1,
		LastError:    err.Error(),
		DeleteSrc:    pair.DeleteSrc,
		NextRetryAt:  as.daemon.calcNextRetry(1),
		CreatedAt:    time.Now(),
	}
	if qerr := as.queue.Save(task); qerr != nil {
		as.logger.Errorf("保存重试任务失败 %s: %v", dstPath, qerr)
		return
	}
	as.daemon.AddTask(task)
	as.logger.Infof("失败任务已入重试队列: %s", dstPath)
}

// waitCopyDone 等复制完成：只看 undone/done/info 三接口与目标状态，不设墙钟超时。
// 提交接口不回 task id，按文件名在 copy/undone 里绑定我方任务拿到 tid 后，
// 转用 info?tid= 精确轮询（同名多任务取首个并打日志公示，目标校验做最终把关）：
//   - 目标存在且大小一致 → 成功（同盘快拷一次命中）；
//   - info 终态失败 → 失败；终态成功 → 验目标；info 404（任务被清）→ 回列表重绑；
//   - 任务出现过、随后两表皆无且目标未就绪，连续达 copyMissLimit 轮 → 判丢失；
//   - 从未见过任务只管等（快拷可能不建任务）；退出只看三接口与 ctx 取消。
func (as *Alissync) waitCopyDone(ctx context.Context, dstPath, fileName string, wantSize int64) bool {
	return waitCopyDoneWith(ctx, as.client, as.logger, as.config.ID, dstPath, fileName, wantSize)
}

// waitCopyDoneWith 等复制完成（独立函数，daemon 重试与 worker 共用）：
// 只看 undone/done/info 三接口与目标状态，不设墙钟超时，详见方法注释。
func waitCopyDoneWith(ctx context.Context, client *alist.AlistClient, logger *logrus.Logger, configID, dstPath, fileName string, wantSize int64) bool {
	checkDest := func() bool {
		existing, err := client.FSGetNoRetry(ctx, dstPath)
		if err != nil || existing == nil {
			return false
		}
		if wantSize > 0 && existing.Size != wantSize {
			return false
		}
		return true
	}
	if checkDest() {
		return true
	}

	findInList := func(done bool) *alist.TaskInfoData {
		tasks, err := client.ListTasks(ctx, "copy", done)
		if err != nil {
			return nil
		}
		for i := range tasks {
			if strings.Contains(tasks[i].Name, fileName) {
				return &tasks[i]
			}
		}
		return nil
	}

	reportProgress := func(p float64) {
		SetCopyProgress(configID, fileName, p)
	}
	reportProgress(0)

	var taskID string
	boundLogged, everBound, missCount := false, false, 0
	ticker := time.NewTicker(copyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if checkDest() {
				return true
			}
			// 已绑定 tid：info?tid= 精确轮询（copy 类型直达，不逐类型试错）
			if taskID != "" {
				info, err := client.TaskInfoByType(ctx, "copy", taskID)
				if err != nil {
					if alist.IsNotFound(err) {
						logger.Debugf("复制任务 %s 已不在后台（被清理），回列表重绑", taskID)
						taskID = ""
						continue
					}
					continue
				}
				if terminal, success := alist.TaskTerminal(info.State); terminal {
					if !success {
						logger.Errorf("复制任务失败 %s：%s %s", taskID, info.State, info.Error)
						removePartialWith(ctx, client, logger, dstPath)
						return false
					}
					return checkDest()
				}
				logger.Debugf("复制进行中 %s：%.1f%%", dstPath, info.Progress)
				reportProgress(info.Progress)
				continue
			}
			// 未绑定：undone 绑新任务，undone 无则查 done 定性
			if task := findInList(false); task != nil {
				taskID = task.ID
				everBound = true
				missCount = 0
				if !boundLogged {
					logger.Debugf("复制 %s 绑定后台任务 %s (%s)", dstPath, task.ID, task.Name)
					boundLogged = true
				}
				continue
			}
			if task := findInList(true); task != nil {
				if terminal, success := alist.TaskTerminal(task.State); terminal {
					if !success {
						logger.Errorf("复制任务失败 %s：%s %s", task.ID, task.State, task.Error)
						removePartialWith(ctx, client, logger, dstPath)
						return false
					}
					return checkDest()
				}
				// done 表里的非终态（理论无）：继续等
				continue
			}
			// 两表皆无：出现过又消失且目标未就绪，累计达限判丢失；
			// 从未出现只管等（快拷可能不建任务）
			if everBound {
				missCount++
				if missCount >= copyMissLimit {
					logger.Errorf("复制任务丢失 %s（连续 %d 轮两表无记录），留待下轮", dstPath, missCount)
					return false
				}
			}
		}
	}
}

// removePartial 清理失败复制可能留下的半截目标文件（尽力而为，失败只告警）。
// 安全前提：走到等待的复制，其目标要么本就不存在，要么覆盖前已删旧文件，
// 因此现存目标只可能是本次残留；清掉后 never/if_newer 下轮才能重新触发复制。
func (as *Alissync) removePartial(ctx context.Context, dstPath string) {
	removePartialWith(ctx, as.client, as.logger, dstPath)
}

func removePartialWith(ctx context.Context, client *alist.AlistClient, logger *logrus.Logger, dstPath string) {
	if err := client.FSRemove(ctx, dstDirFromPath(dstPath), []string{fileNameFromPath(dstPath)}); err != nil {
		if !alist.IsNotFound(err) {
			logger.Warnf("清理残留目标 %s 失败: %v", dstPath, err)
		}
		return
	}
	logger.Infof("已清理失败复制的残留目标: %s", dstPath)
}

// listRecursive 递归列出目录下所有文件
func (as *Alissync) listRecursive(ctx context.Context, dirPath string, waitTime time.Duration) ([]alist.AlistPath, error) {
	var result []alist.AlistPath
	if err := as.listRecursiveInner(ctx, dirPath, waitTime, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (as *Alissync) listRecursiveInner(ctx context.Context, dirPath string, waitTime time.Duration, result *[]alist.AlistPath) error {
	paths, err := as.client.FSListLight(ctx, dirPath)
	if err != nil {
		return err
	}

	if waitTime > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}

	for _, path := range paths {
		if path.IsDir() {
			if err := as.listRecursiveInner(ctx, path.FullPath, waitTime, result); err != nil {
				return err
			}
		} else {
			*result = append(*result, path)
		}
	}

	return nil
}

// replacePrefix 替换路径前缀（按路径段匹配，避免 /movies 误命中 /movies2）
func replacePrefix(path, oldPrefix, newPrefix string) string {
	dst := strings.TrimSuffix(newPrefix, "/")
	if oldPrefix == "/" || strings.TrimSuffix(oldPrefix, "/") == "" {
		return dst + path
	}
	op := strings.TrimSuffix(oldPrefix, "/")
	if path == op || strings.HasPrefix(path, op+"/") {
		return dst + path[len(op):]
	}
	return path
}

// collectDirs 收集所有需要创建的目标目录
func collectDirs(files []alist.AlistPath, srcPrefix, dstPrefix string) []string {
	dirSet := make(map[string]struct{})
	for _, f := range files {
		dstPath := replacePrefix(f.FullPath, srcPrefix, dstPrefix)
		dir := dstDirFromPath(dstPath)
		if dir != "" {
			dirSet[dir] = struct{}{}
		}
	}
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	return dirs
}
