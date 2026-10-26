package feature

// product_assert_helpers_test.go — 商品域 feature 用例共用的失败信息 / JSON 辅助。

import (
	"strings"
	"testing"

	json "go_wp/pkg/jsonx"
)

// oneLine 把多行 HTML 压成一行（失败信息里能看全，grep 也只截一行）。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// tailAround 取 needle 首次出现处前后各 span 字节（诊断用：整页几十 KB，整份打印看不出关键处）。
func tailAround(s, needle string, span int) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return "（未出现）"
	}
	lo, hi := i-span, i+span
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return oneLine(s[lo:hi])
}

// jsonMap 把响应体解成 map（断言只认协议里的键名，不引一整套匿名结构体）。
func jsonMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（%s）", err, string(body))
	}
	return out
}

// asMap 取 map 里的一层对象（缺失即测试失败）。
func asMap(t *testing.T, value any, what string) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s 应是对象，实际 %#v", what, value)
	}
	return m
}

// asList 取 map 里的一层数组（缺失即测试失败）。
func asList(t *testing.T, value any, what string) []any {
	t.Helper()
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("%s 应是数组，实际 %#v", what, value)
	}
	return list
}
