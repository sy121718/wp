package blueprintservice

// blueprint_document_test.go — Page Document 校验的就近单测（审计 CQ-020）。
//
// blueprint 是「用完即弃」的初始化工具，正因为用得少，它的校验坏了不容易被发现：
// 存进库的文档形状不合法，要到构建期才炸，而那时离写入已经很远。

import (
	"encoding/json"
	"errors"
	"testing"

	blueprintenums "go_wp/internal/module/blueprint/enums"
)

// minimalDoc 最小合法页面文档（与 public/test 的样例同形：settings.layout 必填）。
const minimalDoc = "{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[]}"

// TestValidateDocumentAcceptsMinimalValid 最小合法文档被接受并规范化。
func TestValidateDocumentAcceptsMinimalValid(t *testing.T) {
	out, err := validateDocument(json.RawMessage(minimalDoc))
	if err != nil {
		t.Fatalf("最小合法文档应通过: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("应返回规范化后的字节")
	}
	// 规范化：再解析一次应得到同一份结构（不接受散乱字节）。
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("返回的字节应是合法 JSON: %v", err)
	}
}

// TestValidateDocumentRejectsMalformed 非法输入一律 ErrDataInvalid。
func TestValidateDocumentRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"空字节":      "",
		"非 JSON":   "not-json-at-all",
		"JSON 数组":  "[1,2,3]",
		"截断的 JSON": "{\"root\":",
	}
	for name, raw := range cases {
		_, err := validateDocument(json.RawMessage(raw))
		if err == nil {
			t.Errorf("%s: 应被拒绝", name)
			continue
		}
		if !errors.Is(err, errors.New(blueprintenums.ErrDataInvalid)) && err.Error() != blueprintenums.ErrDataInvalid {
			t.Errorf("%s: 错误应为 ErrDataInvalid，实际 %v", name, err)
		}
	}
}

// TestValidateDocumentIsDeterministic 同一输入两次校验得到同一份字节。
//
// 存储字节的确定性是「同文档同产物」的前提之一：这里若随 map 顺序抖动，
// 下游的 hash 比对会时不时报一次「重建要求」，而内容其实一个字没改。
func TestValidateDocumentIsDeterministic(t *testing.T) {
	in := json.RawMessage(minimalDoc)
	first, err := validateDocument(in)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	second, err := validateDocument(in)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("同一输入应得到同一字节:\n%s\n%s", first, second)
	}
}
