// render.go — 验证码图片渲染（仅标准库 image/png 自绘，无外部依赖）。
//
// 能力：GenerateImage 返回 (captcha_id, base64 PNG data URL)。
// 答案 code 只保存在 Store 中供 Verify 校验，不作为返回值外泄给 HTTP 层，
// 从接口契约上杜绝验证码明文回显。
//
// 渲染要素：随机浅色底 + 随机深色干扰线 + 每字符独立颜色、旋转角与位置抖动
// （字形采用内置 5x7 数字点阵，按倍率放大后做简单旋转变换绘制）。
package captcha

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
)

// 默认画布尺寸（Config.Width/Height 未配置时使用，与 Init 默认值一致）。
const (
	defaultImgWidth  = 120
	defaultImgHeight = 40
)

// dataURLPrefix PNG base64 data URL 前缀，可直接赋给 <img src>。
const dataURLPrefix = "data:image/png;base64,"

// digitGlyphs 数字 0-9 的 5x7 点阵字模（'1' 为实心像素）。
var digitGlyphs = [10][7]string{
	{"01110", "10001", "10011", "10101", "11001", "10001", "01110"}, // 0
	{"00100", "01100", "00100", "00100", "00100", "00100", "01110"}, // 1
	{"01110", "10001", "00001", "00010", "00100", "01000", "11111"}, // 2
	{"11111", "00010", "00100", "00010", "00001", "10001", "01110"}, // 3
	{"00010", "00110", "01010", "10010", "11111", "00010", "00010"}, // 4
	{"11111", "10000", "11110", "00001", "00001", "10001", "01110"}, // 5
	{"00110", "01000", "10000", "11110", "10001", "10001", "01110"}, // 6
	{"11111", "10001", "00001", "00010", "00100", "00100", "00100"}, // 7
	{"01110", "10001", "10001", "01110", "10001", "10001", "01110"}, // 8
	{"01110", "10001", "10001", "01111", "00001", "00010", "01100"}, // 9
}

// GenerateImage 生成数字验证码并渲染为 base64 PNG data URL。
// 返回 captcha_id 与图片；答案只存于 Store，接口层绝不下发明文。
func (s *CaptchaService) GenerateImage() (id, image string) {
	id, code := s.Generate()
	return id, renderCodeDataURL(code, s.imgWidth(), s.imgHeight())
}

// imgWidth / imgHeight 读取配置尺寸，未配置时回退默认值。
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

// renderCodeDataURL 把数字验证码渲染为 PNG 并编码为 data URL。
// 调用方需保证 code 仅含数字（与 Generate 的 digit 语义一致）。
func renderCodeDataURL(code string, width, height int) string {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// 1) 随机浅色背景
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

	// 2) 随机深色干扰线
	lineCount := 3 + randInt(0, 3)
	for i := 0; i < lineCount; i++ {
		drawLine(img,
			randInt(0, width), randInt(0, height),
			randInt(0, width), randInt(0, height),
			randDeepColor(100, 170),
		)
	}

	// 3) 逐字符绘制：网格均分画布，字符独立颜色 + 旋转 + 抖动
	n := len(code)
	if n > 0 {
		cellW := width / n
		glyphW, glyphH := 5.0, 7.0
		// 缩放倍率同时受高度与单元格宽度约束，保证字符不出界、不重叠
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
			drawGlyph(img, digit, cx, cy, scale, thickness, angle, randDeepColor(0, 110))
		}
	}

	// 4) PNG 编码 → base64 data URL
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return dataURLPrefix + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// drawGlyph 以 (cx,cy) 为中心绘制旋转后的数字点阵。
// 旋转变为围绕字符中心的简单二维旋转，最近邻落点加方块笔触。
func drawGlyph(img *image.RGBA, digit, cx, cy, scale, thickness int, angle float64, col color.RGBA) {
	glyph := digitGlyphs[digit]
	cos, sin := math.Cos(angle), math.Sin(angle)
	halfW, halfH := 2.5, 3.0 // 字形几何中心（5x7）
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

// drawBlock 以 (x,y) 为中心画 half 大小的实心方块（带边界裁剪）。
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

// drawLine Bresenham 直线（干扰线）。
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

// randDeepColor 随机深色（分量 ∈ [lo, hi)），用于前景与干扰元素。
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
