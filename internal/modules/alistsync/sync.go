package alistsync

import (
	"context"
	"strings"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
)

// syncPair 同步一对源目目录
func (as *Alissync) syncPair(ctx context.Context, pair PairConfig) error {
	as.logger.Infof("开始同步: %s -> %s", pair.Src, pair.Dst)

	waitTime := time.Duration(as.config.WaitTime) * time.Second

	// 递归列出源目录所有文件
	srcFiles, err := as.listRecursive(ctx, pair.Src, waitTime)
	if err != nil {
		return err
	}

	as.logger.Infof("源目录 %s 共 %d 个文件", pair.Src, len(srcFiles))

	// 收集所有需要同步的文件
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

		// 获取源文件直链
		detail, err := as.client.FSGet(ctx, f.FullPath)
		if err != nil {
			as.logger.Warnf("获取文件直链失败 %s: %v", f.FullPath, err)
			continue
		}
		if detail.RawURL == "" {
			as.logger.Warnf("文件 %s 无直链，跳过", f.FullPath)
			continue
		}

		toSync = append(toSync, *detail)
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

	// 逐个提交同步任务
	for _, f := range toSync {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		dstPath := replacePrefix(f.FullPath, pair.Src, pair.Dst)
		dstDir := dstDirFromPath(dstPath)
		fileName := fileNameFromPath(dstPath)

		taskID, err := as.client.FSPut(ctx, dstDir, []alist.FSPutFile{
			{Path: fileName, URL: f.RawURL},
		})
		if err != nil {
			as.logger.Errorf("提交同步任务失败 %s -> %s: %v", f.FullPath, dstPath, err)

			task := &SyncTask{
				ID:           dstPath,
				SyncConfigID: as.config.ID,
				SrcPath:      f.FullPath,
				DstPath:      dstPath,
				RawURL:       f.RawURL,
				State:        "failed",
				Attempts:     1,
				LastError:    err.Error(),
				NextRetryAt:  as.daemon.calcNextRetry(1),
				CreatedAt:    time.Now(),
			}
			as.queue.Save(task)
			as.daemon.AddTask(task)
			continue
		}

		task := &SyncTask{
			ID:           dstPath,
			SyncConfigID: as.config.ID,
			SrcPath:      f.FullPath,
			DstPath:      dstPath,
			RawURL:       f.RawURL,
			AlistTaskID:  taskID,
			State:        "pending",
			Attempts:     0,
			CreatedAt:    time.Now(),
		}
		as.queue.Save(task)
		as.daemon.AddTask(task)
		as.logger.Infof("同步任务已提交: %s -> %s (task: %s)", f.FullPath, dstPath, taskID)
	}

	return nil
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
