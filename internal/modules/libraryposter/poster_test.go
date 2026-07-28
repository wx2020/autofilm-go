package libraryposter

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestComposePoster(t *testing.T) {
	var inputs [][]byte
	for i := 0; i < 4; i++ {
		img := image.NewRGBA(image.Rect(0, 0, 300, 450))
		for y := 0; y < 450; y++ {
			for x := 0; x < 300; x++ {
				img.Set(x, y, color.RGBA{uint8(30 + i*40), uint8(y % 255), uint8(x % 255), 255})
			}
		}
		var b bytes.Buffer
		if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 80}); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, b.Bytes())
	}
	data, err := composePoster(inputs, LibraryConfig{LibraryName: "Movies", Title: "Movies", Subtitle: "Library"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if out.Bounds().Dx() != posterWidth || out.Bounds().Dy() != posterHeight {
		t.Fatalf("size=%v", out.Bounds())
	}
}
