package aihttp

import (
	json "go_wp/pkg/jsonx"
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

func TestParseFabImagesEmptyInput(t *testing.T) {
	empty := mustJSONList(t, []string{"", "   "})
	for _, raw := range []string{"", "   ", "[]", empty} {
		images, labels, ferr := parseFabImages(raw, "")
		if ferr != nil || images != nil || labels != nil {
			t.Fatalf("入参 %q 应视为没有图片，实得 images=%v labels=%v err=%+v", raw, images, labels, ferr)
		}
	}
}

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

func TestParseFabImagesAlignsLabels(t *testing.T) {

	_, labels, ferr := parseFabImages(mustJSONList(t, []string{testPNG, testPNG}), mustJSONList(t, []string{"one.png"}))
	if ferr != nil {
		t.Fatalf("标识少于图片不该报错（只影响那行小字）：%+v", ferr)
	}
	if len(labels) != 2 || labels[0] != "one.png" || labels[1] != fabImageFallbackLabel {
		t.Fatalf("标识应按序补齐，实得 %+v", labels)
	}

	_, labels, ferr = parseFabImages(mustJSONList(t, []string{testPNG}), mustJSONList(t, []string{"a.png", "b.png"}))
	if ferr != nil {
		t.Fatalf("标识多于图片不该报错：%+v", ferr)
	}
	if len(labels) != 1 || labels[0] != "a.png" {
		t.Fatalf("标识应截到与图同长，实得 %+v", labels)
	}
}

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
