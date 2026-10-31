package captcha

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
)

const (
	defaultImgWidth  = 120
	defaultImgHeight = 40
)

const dataURLPrefix = "data:image/png;base64,"

var digitGlyphs = [10][7]string{
	{"01110", "10001", "10011", "10101", "11001", "10001", "01110"},
	{"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	{"01110", "10001", "00001", "00010", "00100", "01000", "11111"},
	{"11111", "00010", "00100", "00010", "00001", "10001", "01110"},
	{"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	{"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	{"00110", "01000", "10000", "11110", "10001", "10001", "01110"},
	{"11111", "10001", "00001", "00010", "00100", "00100", "00100"},
	{"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	{"01110", "10001", "10001", "01111", "00001", "00010", "01100"},
}

func (s *CaptchaService) GenerateImage() (id, image string) {
	id, code := s.Generate()
	return id, renderCodeDataURL(code, s.imgWidth(), s.imgHeight())
}

func (s *CaptchaService) imgWidth() int {
	if s.config.Width > 0 {
		return s.config.Width
	}
	return defaultImgWidth
}

func (s *CaptchaService) imgHeight() int {
	if s.config.Height > 0 {
		return s.config.Height
	}
	return defaultImgHeight
}

func renderCodeDataURL(code string, width, height int) string {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	bg := color.RGBA{
		R: uint8(randInt(200, 246)),
		G: uint8(randInt(200, 246)),
		B: uint8(randInt(200, 246)),
		A: 255,
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bg)
		}
	}

	lineCount := 2 + randInt(0, 2)
	for i := 0; i < lineCount; i++ {
		drawLine(img,
			randInt(0, width), randInt(0, height),
			randInt(0, width), randInt(0, height),
			randDeepColor(180, 220),
		)
	}

	n := len(code)
	if n > 0 {
		cellW := width / n
		glyphW, glyphH := 5.0, 7.0

		scale := int(math.Min(float64(height)*0.6/glyphH, float64(cellW)*0.8/glyphW))
		if scale < 2 {
			scale = 2
		}
		thickness := scale - 1
		if thickness < 1 {
			thickness = 1
		}
		jitter := scale
		if jitter < 1 {
			jitter = 1
		}
		for i := 0; i < n; i++ {
			digit := int(code[i] - '0')
			if digit < 0 || digit > 9 {
				continue
			}
			cx := cellW*i + cellW/2 + randInt(-jitter, jitter+1)
			cy := height/2 + randInt(-jitter, jitter+1)
			angle := float64(randInt(-25, 26)) * math.Pi / 180

			drawGlyph(img, digit, cx, cy, scale, thickness, angle, randDeepColor(0, 60))
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return dataURLPrefix + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func drawGlyph(img *image.RGBA, digit, cx, cy, scale, thickness int, angle float64, col color.RGBA) {
	glyph := digitGlyphs[digit]
	cos, sin := math.Cos(angle), math.Sin(angle)
	halfW, halfH := 2.5, 3.0
	for row := 0; row < 7; row++ {
		for cc := 0; cc < 5; cc++ {
			if glyph[row][cc] != '1' {
				continue
			}
			gx := (float64(cc) + 0.5 - halfW) * float64(scale)
			gy := (float64(row) + 0.5 - halfH) * float64(scale)
			px := cx + int(math.Round(gx*cos-gy*sin))
			py := cy + int(math.Round(gx*sin+gy*cos))
			drawBlock(img, px, py, thickness, col)
		}
	}
}

func drawBlock(img *image.RGBA, x, y, size int, col color.RGBA) {
	half := size / 2
	for dy := -half; dy <= half; dy++ {
		for dx := -half; dx <= half; dx++ {
			px, py := x+dx, y+dy
			if px < 0 || py < 0 || px >= img.Bounds().Dx() || py >= img.Bounds().Dy() {
				continue
			}
			img.Set(px, py, col)
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	bounds := img.Bounds()
	for {
		if x0 >= 0 && y0 >= 0 && x0 < bounds.Dx() && y0 < bounds.Dy() {
			img.Set(x0, y0, col)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func randDeepColor(lo, hi int) color.RGBA {
	return color.RGBA{
		R: uint8(randInt(lo, hi)),
		G: uint8(randInt(lo, hi)),
		B: uint8(randInt(lo, hi)),
		A: 255,
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
