package templates

import (
	"strings"
	"testing"
	"time"
)

func TestFormatTimeShapes(t *testing.T) {
	stamp := time.Date(2026, 10, 18, 19, 34, 56, 0, time.UTC)
	local := stamp.Local().Format("2006-01-02 15:04")
	got := map[string]string{
		"time":    formatTime(stamp),
		"pointer": formatTime(&stamp),
		"rfc3339": formatTime(stamp.Format(time.RFC3339)),
		"spaced":  formatTime("2026-10-18 19:34:56"),
	}
	for name, out := range got {
		if out != local {
			t.Fatalf("%s 应渲染成 %q，实际 %q", name, local, out)
		}
	}
	if out := formatTime(nil); out != "" {
		t.Fatalf("nil 应渲染成空串，实际 %q", out)
	}
	var missing *time.Time
	if out := formatTime(missing); out != "" {
		t.Fatalf("空指针应渲染成空串，实际 %q", out)
	}
	if out := formatTime(""); out != "" {
		t.Fatalf("空串应渲染成空串，实际 %q", out)
	}
	if out := formatTime("不是时间"); out != "不是时间" {
		t.Fatalf("解析失败应原样返回，实际 %q", out)
	}
	if strings.Contains(formatTime(stamp), ":56") {
		t.Fatalf("页面精度只到分钟，不该显示秒：%q", formatTime(stamp))
	}
}
