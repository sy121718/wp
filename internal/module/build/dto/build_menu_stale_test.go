package builddto

import (
	"strings"
	"testing"
)

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

func TestMenuStaleSourceIDNormalizesProject(t *testing.T) {
	upper := "3F0F1E3A-6D9A-4C0F-9A7E-6F6B1C2D3E4F"
	lower := strings.ToLower(upper)
	a, okA := MenuStaleSourceID(upper, "header")
	b, okB := MenuStaleSourceID(lower, "header")
	if !okA || !okB || a != b {
		t.Fatalf("大小写变体必须归一到同一 source_id: %q(%v) vs %q(%v)", a, okA, b, okB)
	}
}

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

func TestSplitMenuStaleSourceIDRejectsMalformed(t *testing.T) {
	pid := "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f"
	for _, sid := range []string{
		"",
		"   ",
		pid,
		":" + "header",
		pid + ":",
		pid + ":header:mobile",
		"not-a-uuid:header",
		pid + ":header_mobile:x",
	} {
		if project, kind, ok := SplitMenuStaleSourceID(sid); ok {
			t.Fatalf("source_id %q 应被拒绝，实际拆出 (%s,%s)", sid, project, kind)
		}
	}
}
