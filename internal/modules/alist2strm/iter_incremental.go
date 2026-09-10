package alist2strm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/akimio/autofilm/pkg/alist"
)

// isTrackedFile 判断文件是否需要纳入快照追踪
// 纯检查逻辑，无副作用（不调用 addProcessedPath / CollectFile）
func (a2s *Alist2Strm) isTrackedFile(path *alist.AlistPath) bool {
	if path.IsDir() {
		return false
	}

	skipFolders := []string{"@eaDir", "Thumbs.db", ".DS_Store"}
	for _, folder := range skipFolders {
		if strings.Contains(path.FullPath, folder) {
			return false
		}
	}

	if a2s.processFileExts[path.Suffix()] {
		return true
	}

	return IsBDMVFile(path)
}

// iterPathLight 递归遍历目录（轻量版），仅调用 FSListLight 收集文件信息
// 与 IterPath 不同：不发起 fs/get 请求、不执行 processFile、无 worker 池
//
// R1：单目录失败只跳过该子树，收集到 failedDirs 并置 incomplete，
// 不再中断整树；根目录本身失败才返回 error（此时回退全量同样会撞墙，由调用方直接返回错误）。
func (a2s *Alist2Strm) iterPathLight(ctx context.Context, dirPath string, waitTime time.Duration) (files []alist.AlistPath, failedDirs []string, incomplete bool, err error) {
	var result []alist.AlistPath
	var failed []string
	if err := a2s.iterPathLightRecursive(ctx, dirPath, waitTime, &result, &failed, 0); err != nil {
		return nil, nil, false, err
	}
	return result, failed, len(failed) > 0, nil
}

func (a2s *Alist2Strm) iterPathLightRecursive(ctx context.Context, dirPath string, waitTime time.Duration, result *[]alist.AlistPath, failed *[]string, depth int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	paths, err := a2s.client.FSListLight(ctx, dirPath)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wrapped := fmt.Errorf("FSList %s: %w", dirPath, err)
		if depth == 0 {
			// 根目录本身不可达：全量同样会撞墙，直接返回错误，不回退、不存快照、不清理
			return wrapped
		}
		a2s.warnf("遍历跳过异常子树: %v", wrapped)
		*failed = append(*failed, dirPath)
		return nil
	}

	if waitTime > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}

	for _, path := range paths {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if path.IsDir() {
			if err := a2s.iterPathLightRecursive(ctx, path.FullPath, waitTime, result, failed, depth+1); err != nil {
				return err
			}
		} else if a2s.isTrackedFile(&path) {
			*result = append(*result, path)
		}
	}

	return nil
}
