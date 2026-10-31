package contenttemplateservice

import (
	json "go_wp/pkg/jsonx"
	"testing"

	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
)

func TestValidEntityTypeRejectsNilRegistry(t *testing.T) {
	svc := &Service{}
	if svc.validEntityType("product") {
		t.Fatal("注册表为 nil 时不应放行任何实体类型（静默放行会让构建期才炸）")
	}
	if svc.validEntityType("") {
		t.Fatal("空类型不应合法")
	}
}

func TestValidateDocumentRejectsMalformed(t *testing.T) {
	svc := &Service{}
	for name, raw := range map[string]string{
		"空字节":    "",
		"非 JSON": "oops",
		"截断":     "{\"root\":",
	} {
		_, err := svc.validateDocument("product", json.RawMessage(raw))
		if err == nil {
			t.Errorf("%s: 应被拒绝", name)
			continue
		}
		if err.Error() != contenttemplateenums.ErrDataInvalid {
			t.Errorf("%s: 期望 ErrDataInvalid，实际 %v", name, err)
		}
	}
}

func TestHashDocumentStableAndDistinct(t *testing.T) {
	a := hashDocument([]byte("{\"root\":[]}"))
	if len(a) != 64 {
		t.Fatalf("应为 sha256 十六进制（64 字符），实际 %d 字符", len(a))
	}
	if a != hashDocument([]byte("{\"root\":[]}")) {
		t.Fatal("同一内容两次哈希应相同")
	}
	if a == hashDocument([]byte("{\"root\":[{\"id\":\"x\"}]}")) {
		t.Fatal("内容不同应得到不同哈希（否则改原文后旧译文不会失效）")
	}
	if hashDocument(nil) != hashDocument([]byte("")) {
		t.Fatal("nil 与空字节应同哈希（都表示没有内容）")
	}
}
