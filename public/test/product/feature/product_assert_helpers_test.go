package feature

import (
	"strings"
	"testing"

	json "go_wp/pkg/jsonx"
)

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

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

func jsonMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（%s）", err, string(body))
	}
	return out
}

func asMap(t *testing.T, value any, what string) map[string]any {
	t.Helper()
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s 应是对象，实际 %#v", what, value)
	}
	return m
}

func asList(t *testing.T, value any, what string) []any {
	t.Helper()
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("%s 应是数组，实际 %#v", what, value)
	}
	return list
}
