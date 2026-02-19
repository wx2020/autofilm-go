package extensions

// AppName 应用名称
const AppName = "AutoFilm Go"

// 文件扩展名常量
var (
	// VideoExts 视频文件后缀
	VideoExts = map[string]bool{
		".mp4":  true,
		".mkv":  true,
		".flv":  true,
		".avi":  true,
		".wmv":  true,
		".ts":   true,
		".rmvb": true,
		".webm": true,
		".mpg":  true,
		".m2ts": true,
		".strm": true, // 扩展视频文件后缀
	}

	// SubtitleExts 字幕文件后缀
	SubtitleExts = map[string]bool{
		".ass": true,
		".srt": true,
		".ssa": true,
		".sub": true,
	}

	// ImageExts 图片文件后缀
	ImageExts = map[string]bool{
		".png": true,
		".jpg": true,
		".jpeg": true,
	}

	// NFOExts NFO文件后缀
	NFOExts = map[string]bool{
		".nfo": true,
	}
)

// IsVideoExt 检查是否为视频文件扩展名
func IsVideoExt(ext string) bool {
	return VideoExts[ext]
}

// IsSubtitleExt 检查是否为字幕文件扩展名
func IsSubtitleExt(ext string) bool {
	return SubtitleExts[ext]
}

// IsImageExt 检查是否为图片文件扩展名
func IsImageExt(ext string) bool {
	return ImageExts[ext]
}

// IsNFOExt 检查是否为NFO文件扩展名
func IsNFOExt(ext string) bool {
	return NFOExts[ext]
}

// GetProcessFileExts 获取需要处理的文件扩展名集合
func GetProcessFileExts(subtitle, image, nfo bool, otherExts []string) map[string]bool {
	exts := make(map[string]bool)

	// 添加视频扩展名
	for k := range VideoExts {
		if k != ".strm" { // .strm 是输出文件，不是输入文件
			exts[k] = true
		}
	}

	// 添加字幕扩展名
	if subtitle {
		for k := range SubtitleExts {
			exts[k] = true
		}
	}

	// 添加图片扩展名
	if image {
		for k := range ImageExts {
			exts[k] = true
		}
	}

	// 添加NFO扩展名
	if nfo {
		for k := range NFOExts {
			exts[k] = true
		}
	}

	// 添加自定义扩展名
	for _, ext := range otherExts {
		if ext != "" {
			// 确保有 .
			if ext[0] != '.' {
				ext = "." + ext
			}
			exts[ext] = true
		}
	}

	return exts
}

// GetDownloadExts 获取需要下载的文件扩展名集合
func GetDownloadExts(subtitle, image, nfo bool, otherExts []string) map[string]bool {
	exts := make(map[string]bool)

	// 添加字幕扩展名
	if subtitle {
		for k := range SubtitleExts {
			exts[k] = true
		}
	}

	// 添加图片扩展名
	if image {
		for k := range ImageExts {
			exts[k] = true
		}
	}

	// 添加NFO扩展名
	if nfo {
		for k := range NFOExts {
			exts[k] = true
		}
	}

	// 添加自定义扩展名
	for _, ext := range otherExts {
		if ext != "" {
			// 确保有 .
			if ext[0] != '.' {
				ext = "." + ext
			}
			exts[ext] = true
		}
	}

	return exts
}
