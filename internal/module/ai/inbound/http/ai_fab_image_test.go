package aihttp

// ai_fab_image_test.go — 图片附件的入口校验。
//
// 这一层的每个拒绝分支都对应一种「不拦就会出事」的形态：
//   · 非 data URI → 服务端替用户去取一个任意地址（SSRF）；
//   · 超张数 / 超体积 → 一次提问把上下文推到上游上限，报错来自上游、用户看不懂；
//   · 标识与图数量对不上 → 界面把 A 图的名字画在 B 图下面（比没有名字更难发现）。

import (
	"encoding/json"
	"strings"
	"testing"

	aienums "go_wp/internal/module/ai/enums"
)

const testPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func mustJSONList(t *testing.T, items []string) string {
	t.Helper()
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("构造测试入参失败：%v", err)
	}
	return string(raw)
}

func TestParseFabImagesAcceptsDataURI(t *testing.T) {
	images, labels, ferr := parseFabImages(
		mustJSONList(t, []string{testPNG}),
		mustJSONList(t, []string{"red.png"}),
	)
	if ferr != nil {
		t.Fatalf("合法的 data URI 不该被拒：%+v", ferr)
	}
	if len(images) != 1 || images[0] != testPNG {
		t.Fatalf("图片没取到：%+v", images)
	}
	if len(labels) != 1 || labels[0] != "red.png" {
		t.Fatalf("标识没取到：%+v", labels)
	}
}

// 空输入与「没带图」必须是同一件事：调用方不该再判一次空数组。
func TestParseFabImagesEmptyInput(t *testing.T) {
	empty := mustJSONList(t, []string{"", "   "})
	for _, raw := range []string{"", "   ", "[]", empty} {
		images, labels, ferr := parseFabImages(raw, "")
		if ferr != nil || images != nil || labels != nil {
			t.Fatalf("入参 %q 应视为没有图片，实得 images=%v labels=%v err=%+v", raw, images, labels, ferr)
		}
	}
}

// 只接受 data URI。
//
// 收下 http(s) 地址等于把「服务端替用户取任意地址」这条链路打开
// （内网、云元数据 169.254.169.254 都在可达范围内），而这条链路
// （浏览器 → 本服务 → 上游模型）根本不需要服务端去取图。
func TestParseFabImagesRejectsRemoteURL(t *testing.T) {
	for _, bad := range []string{
		"https://example.com/a.png",
		"http://169.254.169.254/latest/meta-data/",
		"/uploads/logo.png",
		"data:text/html;base64,PHNjcmlwdD4=",
	} {
		images, _, ferr := parseFabImages(mustJSONList(t, []string{bad}), "")
		if ferr == nil {
			t.Fatalf("非图片 data URI 的入参 %q 必须被拒，实得 %+v", bad, images)
		}
		if ferr.Key != aienums.MsgFabImageBadFormat {
			t.Fatalf("入参 %q 的归口应为格式错误，实得 %q", bad, ferr.Key)
		}
	}
}

func TestParseFabImagesRejectsTooMany(t *testing.T) {
	many := make([]string, fabImageMaxCount+1)
	for i := range many {
		many[i] = testPNG
	}
	_, _, ferr := parseFabImages(mustJSONList(t, many), "")
	if ferr == nil || ferr.Key != aienums.MsgFabImageTooMany {
		t.Fatalf("超过 %d 张应被拒，实得 %+v", fabImageMaxCount, ferr)
	}
}

func TestParseFabImagesRejectsTooLarge(t *testing.T) {
	huge := "data:image/png;base64," + strings.Repeat("A", fabImageMaxBytes)
	_, _, ferr := parseFabImages(mustJSONList(t, []string{huge}), "")
	if ferr == nil || ferr.Key != aienums.MsgFabImageTooLarge {
		t.Fatalf("超过单张上限应被拒，实得 %+v", ferr)
	}
}

// 标识与图**一图对一图**：数量对不上时补齐，不能错位。
//
// 错位的表现是「把 A 图的名字画在 B 图下面」——它看起来完全正常，
// 只有用户自己知道发出去的是哪张。
func TestParseFabImagesAlignsLabels(t *testing.T) {
	// 标识缺少 → 用占位补齐
	_, labels, ferr := parseFabImages(mustJSONList(t, []string{testPNG, testPNG}), mustJSONList(t, []string{"one.png"}))
	if ferr != nil {
		t.Fatalf("标识少于图片不该报错（只影响那行小字）：%+v", ferr)
	}
	if len(labels) != 2 || labels[0] != "one.png" || labels[1] != fabImageFallbackLabel {
		t.Fatalf("标识应按序补齐，实得 %+v", labels)
	}
	// 标识多 → 截断到与图同长
	_, labels, ferr = parseFabImages(mustJSONList(t, []string{testPNG}), mustJSONList(t, []string{"a.png", "b.png"}))
	if ferr != nil {
		t.Fatalf("标识多于图片不该报错：%+v", ferr)
	}
	if len(labels) != 1 || labels[0] != "a.png" {
		t.Fatalf("标识应截到与图同长，实得 %+v", labels)
	}
}

// 标识解不出来不算错误：它只影响界面上那行小字，图本身照发。
func TestParseFabImagesBadLabelsAreNotFatal(t *testing.T) {
	images, labels, ferr := parseFabImages(mustJSONList(t, []string{testPNG}), "not-json")
	if ferr != nil {
		t.Fatalf("标识解析失败不该拦下整条提问：%+v", ferr)
	}
	if len(images) != 1 {
		t.Fatalf("图应照常发出：%+v", images)
	}
	if len(labels) != 1 || labels[0] != fabImageFallbackLabel {
		t.Fatalf("标识应回落到占位，实得 %+v", labels)
	}
}
