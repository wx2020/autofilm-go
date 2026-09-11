package alistsync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
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

	// 逐个服务端直拷，同步等待结果
	var copied, skipped int
	var errs []error
	for _, f := range toSync {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		dstPath := replacePrefix(f.FullPath, pair.Src, pair.Dst)
		srcDir := dstDirFromPath(f.FullPath)
		srcName := fileNameFromPath(f.FullPath)
		dstDir := dstDirFromPath(dstPath)
		dstName := fileNameFromPath(dstPath)

		// 目标已存在且策略允许覆盖时，先删旧目标，避免服务端撞名自动改名
		if existing, err := as.client.FSGetNoRetry(ctx, dstPath); err == nil && existing != nil {
			if err := as.client.FSRemove(ctx, dstDir, []string{dstName}); err != nil {
				as.logger.Errorf("删除已存在的目标 %s 失败: %v", dstPath, err)
				errs = append(errs, fmt.Errorf("删除已存在的目标 %s 失败: %w", dstPath, err))
				continue
			}
		}

		// 单文件本轮直重试：瞬时失败（驱动抖动/提交 EOF）当场重提，
		// 耗尽才记错留待下轮 cron，不把一次抖动拖成一整轮等待。
		if ok, skip := as.copyOneWithRetry(ctx, f.FullPath, srcDir, srcName, dstPath, dstDir, dstName, f.Size); ok {
			copied++
		} else if skip {
			skipped++
		} else {
			errs = append(errs, fmt.Errorf("复制 %s -> %s 失败（已重试），留待下轮", f.FullPath, dstPath))
		}
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

// copyMaxAttempts 单文件本轮直重试次数；copyRetryBackoff 每次重试前等待
const copyMaxAttempts = 3
const copyRetryBackoff = 10 * time.Second

// copyOneWithRetry 单文件复制（含本轮直重试），返回 (成功, 跳过)。
// 源消失记跳过；终态失败/等待丢失先清残留再按退避重提，耗尽返回失败。
func (as *Alissync) copyOneWithRetry(ctx context.Context, srcFullPath, srcDir, srcName, dstPath, dstDir, dstName string, wantSize int64) (bool, bool) {
	fileName := fileNameFromPath(srcFullPath)
	for attempt := 1; attempt <= copyMaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return false, false
		}
		if attempt > 1 {
			as.logger.Infof("重试复制 %s -> %s（第 %d/%d 次）", srcFullPath, dstPath, attempt, copyMaxAttempts)
			select {
			case <-ctx.Done():
				return false, false
			case <-time.After(copyRetryBackoff):
			}
		}
		if err := as.client.FSCopy(ctx, srcDir, dstDir, []string{srcName}); err != nil {
			if alist.IsNotFound(err) {
				as.logger.Warnf("源文件已消失，跳过: %s", srcFullPath)
				return false, true
			}
			// 歧义失败：提交可能已受理，看目标裁决
			if as.waitCopyDone(ctx, dstPath, fileName, wantSize) {
				as.logger.Infof("已复制（状态裁决）: %s -> %s", srcFullPath, dstPath)
				return true, false
			}
			as.removePartial(ctx, dstPath)
			as.logger.Warnf("复制 %s -> %s 第 %d 次未完成，已清残留", srcFullPath, dstPath, attempt)
			continue
		}
		// code 200 仅表示已提交：看目标裁决
		if as.waitCopyDone(ctx, dstPath, fileName, wantSize) {
			as.logger.Infof("已复制: %s -> %s", srcFullPath, dstPath)
			return true, false
		}
		as.removePartial(ctx, dstPath)
		as.logger.Warnf("复制 %s -> %s 第 %d 次未完成，已清残留", srcFullPath, dstPath, attempt)
	}
	as.logger.Errorf("复制 %s -> %s 失败（已重试 %d 次）", srcFullPath, dstPath, copyMaxAttempts)
	return false, false
}

// waitCopyDone 等复制完成：只看 undone/done/info 三接口与目标状态，不设墙钟超时。
// 提交接口不回 task id，按文件名在 copy/undone 里绑定我方任务拿到 tid 后，
// 转用 info?tid= 精确轮询（同名多任务取首个并打日志公示，目标校验做最终把关）：
//   - 目标存在且大小一致 → 成功（同盘快拷一次命中）；
//   - info 终态失败 → 失败；终态成功 → 验目标；info 404（任务被清）→ 回列表重绑；
//   - 任务出现过、随后两表皆无且目标未就绪，连续达 copyMissLimit 轮 → 判丢失；
//   - 从未见过任务只管等（快拷可能不建任务）；退出只看三接口与 ctx 取消。
func (as *Alissync) waitCopyDone(ctx context.Context, dstPath, fileName string, wantSize int64) bool {
	checkDest := func() bool {
		existing, err := as.client.FSGetNoRetry(ctx, dstPath)
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
		tasks, err := as.client.ListTasks(ctx, "copy", done)
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
			// 已绑定 tid：info 精确轮询
			if taskID != "" {
				info, err := as.client.TaskInfo(ctx, taskID)
				if err != nil {
					if alist.IsNotFound(err) {
						as.logger.Debugf("复制任务 %s 已不在后台（被清理），回列表重绑", taskID)
						taskID = ""
						continue
					}
					continue
				}
				if terminal, success := alist.TaskTerminal(info.State); terminal {
					if !success {
						as.logger.Errorf("复制任务失败 %s：%s %s", taskID, info.State, info.Error)
						as.removePartial(ctx, dstPath)
						return false
					}
					return checkDest()
				}
				as.logger.Debugf("复制进行中 %s：%.1f%%", dstPath, info.Progress)
				continue
			}
			// 未绑定：undone 绑新任务，undone 无则查 done 定性
			if task := findInList(false); task != nil {
				taskID = task.ID
				everBound = true
				missCount = 0
				if !boundLogged {
					as.logger.Debugf("复制 %s 绑定后台任务 %s (%s)", dstPath, task.ID, task.Name)
					boundLogged = true
				}
				continue
			}
			if task := findInList(true); task != nil {
				if terminal, success := alist.TaskTerminal(task.State); terminal {
					if !success {
						as.logger.Errorf("复制任务失败 %s：%s %s", task.ID, task.State, task.Error)
						as.removePartial(ctx, dstPath)
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
					as.logger.Errorf("复制任务丢失 %s（连续 %d 轮两表无记录），留待下轮", dstPath, missCount)
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
	if err := as.client.FSRemove(ctx, dstDirFromPath(dstPath), []string{fileNameFromPath(dstPath)}); err != nil {
		if !alist.IsNotFound(err) {
			as.logger.Warnf("清理残留目标 %s 失败: %v", dstPath, err)
		}
		return
	}
	as.logger.Infof("已清理失败复制的残留目标: %s", dstPath)
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
