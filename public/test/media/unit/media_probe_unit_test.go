package unit

import (
	"path"
	"regexp"
	"testing"
)

var variantRefRe = regexp.MustCompile(`_(?:thumb|small|medium|full)-\d+-[0-9a-f]{8}\.jpg$`)

func TestProbeImageVariants(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "photo.png", 1600, 900)
	id := seedVariantAttachment(t, db, "photo.png", "photo.png")
	if _, err := svc.GenerateVariants(t.Context(), id); err != nil {
		t.Fatalf("生成变体失败: %v", err)
	}

	got := svc.ProbeImageVariants(t.Context(), "/storage/photo.png")

	wantWidths := []int{320, 768, 1280}
	if len(got) != len(wantWidths) {
		t.Fatalf("候选数 = %d（%v），期望 %d（%v）", len(got), got, len(wantWidths), wantWidths)
	}
	for i, w := range wantWidths {
		if got[i].Width != w {
			t.Fatalf("第 %d 个候选宽度 = %d，期望 %d", i, got[i].Width, w)
		}

		if !variantRefRe.MatchString(path.Base(got[i].URL)) {
			t.Fatalf("第 %d 个候选 URL 不符合带指纹命名: %q", i, got[i].URL)
		}
	}

	for _, url := range []string{
		"",
		"https://cdn.example.com/a.jpg",
		"/storage/../../etc/passwd",
		"/storage/missing.png",
	} {
		if refs := svc.ProbeImageVariants(t.Context(), url); refs != nil {
			t.Errorf("URL %q 应返回 nil，实际 %v", url, refs)
		}
	}
}
