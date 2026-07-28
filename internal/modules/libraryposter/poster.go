package libraryposter

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	posterWidth  = 1200
	posterHeight = 1800
)

func composePoster(images [][]byte, cfg LibraryConfig, titleFontPath, subtitleFontPath string) ([]byte, error) {
	if len(images) == 0 {
		return nil, fmt.Errorf("没有可用海报")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, posterWidth, posterHeight))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{15, 18, 24, 255}}, image.Point{}, draw.Src)

	decoded := make([]image.Image, 0, len(images))
	for _, data := range images {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err == nil {
			decoded = append(decoded, img)
		}
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("海报图片均无法解码")
	}

	// 顶部主视觉采用第一张图片横向裁切。
	drawCover(canvas, image.Rect(0, 0, posterWidth, 760), decoded[0])

	// 下方三列拼图。
	const cols = 3
	cellW, cellH := posterWidth/cols, 520
	maxTiles := minInt(len(decoded), 6)
	for i := 0; i < maxTiles; i++ {
		row, col := i/cols, i%cols
		rect := image.Rect(col*cellW, 760+row*cellH, (col+1)*cellW, 760+(row+1)*cellH)
		drawCover(canvas, rect, decoded[i])
	}

	// 底部渐变提高文字可读性。
	for y := 1180; y < posterHeight; y++ {
		alpha := uint8(float64(y-1180) / float64(posterHeight-1180) * 235)
		draw.Draw(canvas, image.Rect(0, y, posterWidth, y+1), &image.Uniform{C: color.RGBA{8, 10, 15, alpha}}, image.Point{}, draw.Over)
	}

	titleFace, titleClose := loadFontFace(titleFontPath, 82)
	defer titleClose()
	subtitleFace, subtitleClose := loadFontFace(subtitleFontPath, 38)
	defer subtitleClose()

	title := cfg.Title
	if title == "" {
		title = cfg.LibraryName
	}
	drawCenteredText(canvas, titleFace, title, 1510, color.White)
	if cfg.Subtitle != "" {
		drawCenteredText(canvas, subtitleFace, cfg.Subtitle, 1590, color.RGBA{220, 225, 235, 255})
	}

	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decodeImage(data []byte) (image.Image, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err == nil {
		return img, nil
	}
	if format == "jpeg" {
		return jpeg.Decode(bytes.NewReader(data))
	}
	return nil, err
}

func drawCover(dst *image.RGBA, rect image.Rectangle, src image.Image) {
	sb := src.Bounds()
	srcRatio := float64(sb.Dx()) / float64(sb.Dy())
	dstRatio := float64(rect.Dx()) / float64(rect.Dy())
	crop := sb
	if srcRatio > dstRatio {
		width := int(float64(sb.Dy()) * dstRatio)
		left := sb.Min.X + (sb.Dx()-width)/2
		crop = image.Rect(left, sb.Min.Y, left+width, sb.Max.Y)
	} else {
		height := int(float64(sb.Dx()) / dstRatio)
		top := sb.Min.Y + (sb.Dy()-height)/2
		crop = image.Rect(sb.Min.X, top, sb.Max.X, top+height)
	}
	xdraw.CatmullRom.Scale(dst, rect, src, crop, draw.Over, nil)
}

func loadFontFace(path string, size float64) (font.Face, func()) {
	data, err := os.ReadFile(path)
	if err != nil {
		return basicfont.Face7x13, func() {}
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return basicfont.Face7x13, func() {}
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 96, Hinting: font.HintingFull})
	if err != nil {
		return basicfont.Face7x13, func() {}
	}
	return face, func() { _ = face.Close() }
}

func drawCenteredText(dst draw.Image, face font.Face, text string, baseline int, c color.Color) {
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: face}
	width := d.MeasureString(text).Ceil()
	d.Dot = fixed.P((posterWidth-width)/2, baseline)
	d.DrawString(text)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
