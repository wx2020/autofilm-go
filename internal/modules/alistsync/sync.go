package alistsync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
)

// syncPair 同步一对源目目录（同实例服务端直拷，不走离线下载）
// 语义：按覆盖策略逐文件 FSCopy，调用返回即代表服务端复制完成，
// 不再提交异步离线任务、不写重试队列，Run 返回就是真实完成。
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

		if err := as.client.FSCopy(ctx, srcDir, dstDir, []string{srcName}); err != nil {
			if alist.IsNotFound(err) {
				as.logger.Warnf("源文件已消失，跳过: %s", f.FullPath)
				skipped++
				continue
			}
			as.logger.Errorf("复制失败 %s -> %s: %v", f.FullPath, dstPath, err)
			errs = append(errs, fmt.Errorf("复制 %s -> %s 失败: %w", f.FullPath, dstPath, err))
			continue
		}
		copied++
		as.logger.Infof("已复制: %s -> %s", f.FullPath, dstPath)
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
