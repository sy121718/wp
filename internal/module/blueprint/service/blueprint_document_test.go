package blueprintservice

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"testing"

	blueprintenums "go_wp/internal/module/blueprint/enums"
)

const minimalDoc = "{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[]}"

func TestValidateDocumentAcceptsMinimalValid(t *testing.T) {
	out, err := validateDocument(json.RawMessage(minimalDoc))
	if err != nil {
		t.Fatalf("最小合法文档应通过: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("应返回规范化后的字节")
	}

	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("返回的字节应是合法 JSON: %v", err)
	}
}

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
