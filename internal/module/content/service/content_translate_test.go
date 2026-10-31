package contentservice

import (
	"context"
	"strings"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	"go_wp/pkg/i18n"
)

type stubStore struct {
	targets map[string]string
}

func (s *stubStore) LoadTargets(_ context.Context, lang string, hashes []string) (map[string]string, error) {
	out := map[string]string{}
	for _, h := range hashes {
		if v, ok := s.targets[h+"|"+lang]; ok {

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

		i18n.ContentHash("/uploads/a.jpg") + "|en-US": "/uploads/WRONG.jpg",
	}}}
	data := map[string]any{
		"title":          "中文标题",
		"excerpt":        "中文标题",
		"featuredImage":  "/uploads/a.jpg",
		"focusKeyword":   "中文标题",
		"seoTitle":       "中文标题",
		"seoDescription": "中文标题",
		"status":         "published",
		"viewCount":      float64(3),
	}
	out := svc.translateData(context.Background(), "en-US", "article", data)

	for _, f := range []string{"title", "excerpt"} {
		if out[f] != "English title" {
			t.Errorf("%s 应取到译文，实际 %q", f, out[f])
		}
	}

	for _, f := range []string{"seoTitle", "seoDescription"} {
		if out[f] != "中文标题" {
			t.Errorf("%s 不该参与翻译，实际 %q", f, out[f])
		}
	}

	if out["featuredImage"] != "/uploads/a.jpg" {
		t.Errorf("图片地址不该翻译（翻了会指向不存在的文件），实际 %q", out["featuredImage"])
	}
	if out["focusKeyword"] != "中文标题" {
		t.Errorf("focusKeyword 不进产物、不该翻译，实际 %q", out["focusKeyword"])
	}
	if out["status"] != "published" || out["viewCount"] != float64(3) {
		t.Errorf("非字符串 / 非白名单字段应原样保留，实际 %v / %v", out["status"], out["viewCount"])
	}

	if data["title"] != "中文标题" {
		t.Errorf("不应改写入参 map，实际 %q", data["title"])
	}
}

func TestTranslateDataNoTranslationFallsBack(t *testing.T) {
	svc := &Service{contentStore: &stubStore{targets: map[string]string{}}}
	data := map[string]any{"title": "中文标题"}
	out := svc.translateData(context.Background(), "en-US", "article", data)
	if out["title"] != "中文标题" {
		t.Fatalf("无译文应回退原文，实际 %q", out["title"])
	}
}

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

func TestTranslateDataKeepsPlainTextUntouched(t *testing.T) {
	svc := &Service{contentStore: &stubStore{targets: map[string]string{
		i18n.ContentHash("中文标题") + "|en-US": "A < B & C",
	}}}
	out := svc.translateData(context.Background(), "en-US", "article", map[string]any{"title": "中文标题"})
	if out["title"] != "A < B & C" {
		t.Fatalf("纯文本字段不应被转义，实际 %q", out["title"])
	}
}

func TestTranslatableFieldsExcludeAssets(t *testing.T) {
	for _, f := range []string{"featuredImage", "focusKeyword", "seoTitle", "seoDescription"} {
		if contentcontract.IsTranslatableField("article", f) {
			t.Errorf("%s 不应在可翻译字段里", f)
		}
	}
	got := contentcontract.TranslatableFields("article")
	if len(got) != 3 {
		t.Fatalf("article 应有 3 个可翻译字段（title / body / excerpt），实际 %v", got)
	}
}
