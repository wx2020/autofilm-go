package alist2strm

// Alist2StrmMode Alist2Strm运行模式
type Alist2StrmMode string

const (
	// AlistURLMode AlistURL模式 - 使用Alist下载链接
	AlistURLMode Alist2StrmMode = "AlistURL"
	// RawURLMode RawURL模式 - 使用原始直链
	RawURLMode Alist2StrmMode = "RawURL"
	// AlistPathMode AlistPath模式 - 使用Alist路径
	AlistPathMode Alist2StrmMode = "AlistPath"
)

// FromStr 从字符串转换为Alist2StrmMode
func FromStr(modeStr string) Alist2StrmMode {
	switch modeStr {
	case "AlistURL", "alisturl", "ALISTURL":
		return AlistURLMode
	case "RawURL", "rawurl", "RAWURL":
		return RawURLMode
	case "AlistPath", "alistpath", "ALISTPATH":
		return AlistPathMode
	default:
		return AlistURLMode
	}
}
