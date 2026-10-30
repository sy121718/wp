package builddto

// build_menu_stale_test.go — source_id 的「工程:位置」编解码（纯逻辑）。
//
// 往返是唯一的正确性判据：worker 消费时必须能凭 source_id 精确还原工程与位置，
// 任何偏差都意味着「读别人的数据」或「找错位置」，两者都只能靠拒绝来兜住。

import (
	"strings"
	"testing"
)

// TestMenuStaleSourceIDRoundTrip 四个位置都能拼出来、再原样拆回来。
func TestMenuStaleSourceIDRoundTrip(t *testing.T) {
	pid := "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f"
	for _, kind := range []string{"header", "header_mobile", "footer", "footer_mobile"} {
		sid, ok := MenuStaleSourceID(pid, kind)
		if !ok {
			t.Fatalf("位置 %q 应能组装 source_id", kind)
		}
		gotProject, gotKind, ok := SplitMenuStaleSourceID(sid)
		if !ok {
			t.Fatalf("位置 %q 的 source_id %q 拆不回来", kind, sid)
		}
		if gotProject != pid || gotKind != kind {
			t.Fatalf("往返不一致: got=(%s,%s) want=(%s,%s)", gotProject, gotKind, pid, kind)
		}
	}
}

// TestMenuStaleSourceIDNormalizesProject 同一工程的大小写变体必须落到同一个 source_id。
//
// 否则同一工程在队列里有两个来源，uq_build_jobs_running_source 与 pending 去重双双失效。
func TestMenuStaleSourceIDNormalizesProject(t *testing.T) {
	upper := "3F0F1E3A-6D9A-4C0F-9A7E-6F6B1C2D3E4F"
	lower := strings.ToLower(upper)
	a, okA := MenuStaleSourceID(upper, "header")
	b, okB := MenuStaleSourceID(lower, "header")
	if !okA || !okB || a != b {
		t.Fatalf("大小写变体必须归一到同一 source_id: %q(%v) vs %q(%v)", a, okA, b, okB)
	}
}

// TestMenuStaleSourceIDRejectsInvalid 非法工程或空位置必须拒绝，不许拼出半个 id。
func TestMenuStaleSourceIDRejectsInvalid(t *testing.T) {
	for _, c := range []struct{ project, kind string }{
		{"", "header"},
		{"not-a-uuid", "header"},
		{"3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", ""},
		{"3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", "  "},
		{"3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f", "header:mobile"},
	} {
		if sid, ok := MenuStaleSourceID(c.project, c.kind); ok {
			t.Fatalf("(%q,%q) 应被拒绝，实际得到 %q", c.project, c.kind, sid)
		}
	}
}

// TestSplitMenuStaleSourceIDRejectsMalformed 消费侧只认严格形状，其余一律拒绝。
func TestSplitMenuStaleSourceIDRejectsMalformed(t *testing.T) {
	pid := "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f"
	for _, sid := range []string{
		"",
		"   ",
		pid,                      // 没有位置
		":" + "header",           // 没有工程
		pid + ":",                // 没有位置
		pid + ":header:mobile",   // 多了冒号
		"not-a-uuid:header",      // 工程不是 uuid
		pid + ":header_mobile:x", // 三段
	} {
		if project, kind, ok := SplitMenuStaleSourceID(sid); ok {
			t.Fatalf("source_id %q 应被拒绝，实际拆出 (%s,%s)", sid, project, kind)
		}
	}
}
