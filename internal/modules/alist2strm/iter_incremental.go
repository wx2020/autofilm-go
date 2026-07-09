package alist2strm

import (
	"context"
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
func (a2s *Alist2Strm) iterPathLight(ctx context.Context, dirPath string, waitTime time.Duration) ([]alist.AlistPath, error) {
	var result []alist.AlistPath
	if err := a2s.iterPathLightRecursive(ctx, dirPath, waitTime, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a2s *Alist2Strm) iterPathLightRecursive(ctx context.Context, dirPath string, waitTime time.Duration, result *[]alist.AlistPath) error {
	paths, err := a2s.client.FSListLight(ctx, dirPath)
	if err != nil {
		return err
	}

	if waitTime > 0 {
		time.Sleep(waitTime)
	}

	for _, path := range paths {
		if path.IsDir() {
			if err := a2s.iterPathLightRecursive(ctx, path.FullPath, waitTime, result); err != nil {
				return err
			}
		} else if a2s.isTrackedFile(&path) {
			*result = append(*result, path)
		}
	}

	return nil
}
