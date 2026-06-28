package extensions

import "github.com/akimio/autofilm/internal/core"

// PrintBanner 打印 App 名称与版本横幅，用于程序启动与退出时显示
func PrintBanner(version string) {
	center := "═════════════════════════════════════════════════════════════════"
	centerText := " " + core.AppName + " " + version + " "

	// 按 rune 切片对齐，避免把多字节 UTF-8 字符从中间切断产生乱码
	centerRunes := []rune(center)
	textRunes := []rune(centerText)

	// 简单居中对齐
	padding := (len(centerRunes) - len(textRunes)) / 2
	if padding < 0 {
		padding = 0
	}

	// 文本宽度溢出时，只用文本
	if padding+len(textRunes) > len(centerRunes) {
		println(centerText)
		return
	}

	result := string(centerRunes[:padding]) + centerText + string(centerRunes[padding+len(textRunes):])
	println(result)
}