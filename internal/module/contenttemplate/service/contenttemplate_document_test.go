package contenttemplateservice

// contenttemplate_document_test.go — 模板文档校验与哈希的就近单测（审计 CQ-020）。
//
// 模板文档是构建的直接输入，校验松一格，坏文档就会一路走到构建期；
// hashDocument 决定「改原文后旧译文/旧快照是否失效」，算错等于静默沿用旧内容。

import (
	"encoding/json"
	"testing"

	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
)

// TestValidEntityTypeRejectsNilRegistry 注册表缺失一律不合法（fail-closed）。
func TestValidEntityTypeRejectsNilRegistry(t *testing.T) {
	svc := &Service{}
	if svc.validEntityType("product") {
		t.Fatal("注册表为 nil 时不应放行任何实体类型（静默放行会让构建期才炸）")
	}
	if svc.validEntityType("") {
		t.Fatal("空类型不应合法")
	}
}

// TestValidateDocumentRejectsMalformed 非法文档在校验期被拒，不进入存储。
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

// TestHashDocumentStableAndDistinct 文档哈希稳定且对内容敏感。
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
