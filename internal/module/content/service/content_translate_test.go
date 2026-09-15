package contentservice

// content_translate_test.go — 内容字段取词（审计 I18N-006）。
//
// 重点不是「翻译生效了」，而是**哪些字段没被翻译**：可翻译清单一旦放宽，
// 坏的是产物本身（图片地址被替换成中文 → 图片 404），而且只在非默认语言的站点上出现。

import (
	"context"
	"strings"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	"go_wp/pkg/i18n"
)

// stubStore 按 (hash, lang) 返回预设译文。
type stubStore struct {
	targets map[string]string
}

func (s *stubStore) LoadTargets(_ context.Context, lang string, hashes []string) (map[string]string, error) {
	out := map[string]string{}
	for _, h := range hashes {
		if v, ok := s.targets[h+"|"+lang]; ok {
			// 语境由调用方拼（article.title 等），这里按所有已知字段各放一份。
			for _, f := range []string{"title", "body", "excerpt", "seoTitle", "seoDescription"} {
				out[i18n.ContentIndexKey(h, i18n.ContentContext("article", f))] = v
			}
		}
	}
	return out, nil
}

func TestTranslateDataFieldSelection(t *testing.T) {
	svc := &Service{contentStore: &stubStore{targets: map[string]string{
		i18n.ContentHash("中文标题") + "|en-US": "English title",
		// featuredImage 的原文也准备一份译文：如果实现误把它当可翻译字段，
		// 这里就会命中并把图片地址换掉 —— 断言随之失败。
		i18n.ContentHash("/uploads/a.jpg") + "|en-US": "/uploads/WRONG.jpg",
	}}}
	data := map[string]any{
		"title":          "中文标题",
		"excerpt":        "中文标题", // 命中同一份译文，验证多字段各自查语境
		"featuredImage":  "/uploads/a.jpg",
		"focusKeyword":   "中文标题",
		"seoTitle":       "中文标题",
		"seoDescription": "中文标题",
		"status":         "published", // 非白名单字段
		"viewCount":      float64(3),  // 非字符串
	}
	out := svc.translateData(context.Background(), "en-US", "article", data)

	for _, f := range []string{"title", "excerpt", "seoTitle", "seoDescription"} {
		if out[f] != "English title" {
			t.Errorf("%s 应取到译文，实际 %q", f, out[f])
		}
	}
	// body 不在 data 里，跳过；下面两个是**刻意不翻**的字段。
	if out["featuredImage"] != "/uploads/a.jpg" {
		t.Errorf("图片地址不该翻译（翻了会指向不存在的文件），实际 %q", out["featuredImage"])
	}
	if out["focusKeyword"] != "中文标题" {
		t.Errorf("focusKeyword 不进产物、不该翻译，实际 %q", out["focusKeyword"])
	}
	if out["status"] != "published" || out["viewCount"] != float64(3) {
		t.Errorf("非字符串 / 非白名单字段应原样保留，实际 %v / %v", out["status"], out["viewCount"])
	}
	// 返回副本：原 map 不能被改写（同一份 data 可能被多处引用）。
	if data["title"] != "中文标题" {
		t.Errorf("不应改写入参 map，实际 %q", data["title"])
	}
}

// TestTranslateDataNoTranslationFallsBack 无译文时逐字回退原文。
func TestTranslateDataNoTranslationFallsBack(t *testing.T) {
	svc := &Service{contentStore: &stubStore{targets: map[string]string{}}}
	data := map[string]any{"title": "中文标题"}
	out := svc.translateData(context.Background(), "en-US", "article", data)
	if out["title"] != "中文标题" {
		t.Fatalf("无译文应回退原文，实际 %q", out["title"])
	}
}

// TestTranslateDataSanitizesRichText 富文本译文的白名单清洗（审计 I18N-006 验收 3）。
//
// 译文和正文一样是**不可信输入**：原文过清洗不代表译文也干净 —— 工作台是另一个入口，
// AI / PO 导入的译文更要过这道关。少了这步，一个被污染的译文就能把脚本带进所有语言版本。
func TestTranslateDataSanitizesRichText(t *testing.T) {
	svc := &Service{contentStore: &stubStore{targets: map[string]string{
		i18n.ContentHash("<p>正文</p>") + "|en-US": "<p>Body</p><script>alert(1)</script>",
	}}}
	out := svc.translateData(context.Background(), "en-US", "article", map[string]any{
		"body": "<p>正文</p>",
	})
	body, _ := out["body"].(string)
	if strings.Contains(body, "<script") {
		t.Fatalf("译文里的脚本应被清洗掉，实际 %q", body)
	}
	if !strings.Contains(body, "Body") {
		t.Fatalf("清洗不应删掉正文内容，实际 %q", body)
	}
}

// TestTranslateDataKeepsPlainTextUntouched 纯文本字段不做 HTML 清洗。
//
// 标题里写「A < B」是合法内容，过清洗会把它转义成 A &lt; B —— 页面上看到的是原文之外的字符。
func TestTranslateDataKeepsPlainTextUntouched(t *testing.T) {
	svc := &Service{contentStore: &stubStore{targets: map[string]string{
		i18n.ContentHash("中文标题") + "|en-US": "A < B & C",
	}}}
	out := svc.translateData(context.Background(), "en-US", "article", map[string]any{"title": "中文标题"})
	if out["title"] != "A < B & C" {
		t.Fatalf("纯文本字段不应被转义，实际 %q", out["title"])
	}
}

// TestTranslatableFieldsExcludeAssets 清单本身守一遍：资源与评分器字段不在可翻译集合里。
func TestTranslatableFieldsExcludeAssets(t *testing.T) {
	for _, f := range []string{"featuredImage", "focusKeyword"} {
		if contentcontract.IsTranslatableField("article", f) {
			t.Errorf("%s 不应在可翻译字段里", f)
		}
	}
	got := contentcontract.TranslatableFields("article")
	if len(got) != 5 {
		t.Fatalf("article 应有 5 个可翻译字段，实际 %v", got)
	}
}
