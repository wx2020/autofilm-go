package alistsync

import (
	"time"

	"github.com/akimio/autofilm/pkg/alist"
)

// OverwritePolicy 覆盖策略
type OverwritePolicy string

const (
	OverwriteNever   OverwritePolicy = "never"
	OverwriteAlways  OverwritePolicy = "always"
	OverwriteIfNewer OverwritePolicy = "if_newer"
)

// ShouldOverwrite 判断是否应覆盖目标文件
// existing: 目标位置已存在的文件信息（nil 表示不存在）
// 空策略默认按 if_newer 处理：缺失必同步，存在比对 mtime，避免空配置静默不同步
func ShouldOverwrite(policy OverwritePolicy, src, existing *alist.AlistPath) bool {
	if policy == "" {
		policy = OverwriteIfNewer
	}
	switch policy {
	case OverwriteAlways:
		return true
	case OverwriteNever:
		return false
	case OverwriteIfNewer:
		if existing == nil {
			return true
		}
		srcModified := parseTime(src.Modified)
		dstModified := parseTime(existing.Modified)
		return srcModified.After(dstModified)
	default:
		return false
	}
}

func parseTime(s string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05.000000Z", s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05Z", s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}
