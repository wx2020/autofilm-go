package alist2strm

import (
	"path/filepath"
	"sort"

	"github.com/akimio/autofilm/pkg/alist"
)

// BDMVCollection BDMV文件集合
type BDMVCollection struct {
	Files       []*alist.AlistPath
	LargestFile *alist.AlistPath
	TotalSize   int64
}

// BDMVManager BDMV文件管理器
type BDMVManager struct {
	collections map[string]*BDMVCollection // BDMV根目录 -> 集合
}

// NewBDMVManager 创建BDMV管理器
func NewBDMVManager() *BDMVManager {
	return &BDMVManager{
		collections: make(map[string]*BDMVCollection),
	}
}

// IsBDMVFile 检查是否为BDMV文件
func IsBDMVFile(path *alist.AlistPath) bool {
	fullPath := path.FullPath
	return contains(fullPath, "/BDMV/STREAM/") && path.Suffix() == ".m2ts"
}

// GetBDMVRootDir 获取BDMV根目录
func GetBDMVRootDir(path *alist.AlistPath) string {
	fullPath := path.FullPath
	idx := indexOf(fullPath, "/BDMV/")
	if idx == -1 {
		return ""
	}
	return fullPath[:idx]
}

// GetMovieTitleFromBDMVPath 从BDMV路径提取电影标题
func GetMovieTitleFromBDMVPath(bdmvRoot string) string {
	return filepath.Base(bdmvRoot)
}

// CollectFile 收集BDMV文件
func (bm *BDMVManager) CollectFile(path *alist.AlistPath) {
	bdmvRoot := GetBDMVRootDir(path)
	if bdmvRoot == "" {
		return
	}

	if _, exists := bm.collections[bdmvRoot]; !exists {
		bm.collections[bdmvRoot] = &BDMVCollection{
			Files: make([]*alist.AlistPath, 0),
		}
	}

	collection := bm.collections[bdmvRoot]
	collection.Files = append(collection.Files, path)
	collection.TotalSize += path.Size
}

// Finalize 完成收集，确定每个BDMV目录的最大文件
func (bm *BDMVManager) Finalize() {
	for _, collection := range bm.collections {
		if len(collection.Files) == 0 {
			continue
		}

		// 按大小排序
		sort.Slice(collection.Files, func(i, j int) bool {
			return collection.Files[i].Size > collection.Files[j].Size
		})

		collection.LargestFile = collection.Files[0]
	}
}

// GetLargestFiles 获取所有BDMV目录的最大文件
func (bm *BDMVManager) GetLargestFiles() []*alist.AlistPath {
	result := make([]*alist.AlistPath, 0, len(bm.collections))
	for _, collection := range bm.collections {
		if collection.LargestFile != nil {
			result = append(result, collection.LargestFile)
		}
	}
	return result
}

// ShouldProcess 检查是否应该处理该BDMV文件（是否为最大文件）
func (bm *BDMVManager) ShouldProcess(path *alist.AlistPath) bool {
	bdmvRoot := GetBDMVRootDir(path)
	if bdmvRoot == "" {
		return false
	}

	collection, exists := bm.collections[bdmvRoot]
	if !exists || collection.LargestFile == nil {
		return false
	}

	return collection.LargestFile.FullPath == path.FullPath
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) != -1
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
