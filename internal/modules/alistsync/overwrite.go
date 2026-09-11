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
		// 缺失仍需复制一份，存在才跳过；否则 never 永远不同步
		return existing == nil
	case OverwriteIfNewer:
		if existing == nil {
			return true
		}
		srcModified := parseTime(src.Modified)
		dstModified := parseTime(existing.Modified)
		if srcModified.IsZero() || dstModified.IsZero() {
			// 时间不可比（格式未知）：退化为大小比对，大小一致视为已同步，
			// 避免一方零时间导致“永远更新/永远跳过”
			return src.Size != existing.Size
		}
		return srcModified.After(dstModified)
	default:
		return false
	}
}

func parseTime(s string) time.Time {
	// RFC3339Nano 优先：覆盖 Z、+08:00 等时区偏移及小数秒（线上真实格式如 2026-09-08T23:19:04+08:00）；
	// 老格式保留兼容。解析失败返回零时间，调用方按“未知”处理而非当作最旧。
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
