package extensions

import "github.com/akimio/autofilm/internal/core"

// Logo 应用启动时显示的 Logo
const Logo = `
      _           _            ____ ___
     | |         | |          |  _ \\_ _|
  ___| |__   __ _| | ___ _   _| |_) | |
 / __| '_ \\ / _  | |/ __| | | |  _ <| |
| (__| | | | (_| | | (__| |_| | |_) | |
 \___|_| |_|\\__,_|_|\\___|\\__, |____/___|
                          __/ |
                         |___/
`

// PrintLogo 打印 Logo
func PrintLogo(version string) {
	println(Logo)
	center := "═════════════════════════════════════════════════════════════════"
	centerText := " " + core.AppName + " " + version + " "

	// 简单居中对齐
	padding := (len(center) - len(centerText)) / 2
	if padding < 0 {
		padding = 0
	}

	println(center[:padding] + centerText + center[padding+len(centerText):])
	println()
}
