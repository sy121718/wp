package unit

// media_probe_unit_test.go — 构建期图片变体探测（页面 srcset 的数据源）单元测试。
//
// ProbeImageVariants 是「构建期响应式图片」的注入点：page 装配层把它作为
// builder.WithAssetProbe 传入编译内核，只有媒体库且变体已就绪的 URL 才输出 srcset。
import (
	"testing"
)

// TestProbeImageVariants 只返回已就绪且能映射到变体文件的宽度；
// 非媒体库 URL、路径穿越、附件不存在、空串一律 nil（调用方不输出 srcset）。
func TestProbeImageVariants(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "photo.png", 1600, 900)
	id := seedVariantAttachment(t, db, "photo.png", "photo.png")
	if _, err := svc.GenerateVariants(t.Context(), id); err != nil {
		t.Fatalf("生成变体失败: %v", err)
	}

	got := svc.ProbeImageVariants(t.Context(), "/storage/photo.png")
	want := []int{320, 1280}
	if len(got) != len(want) {
		t.Fatalf("宽度列表 = %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("宽度列表 = %v，期望 %v", got, want)
		}
	}

	for _, url := range []string{
		"",
		"https://cdn.example.com/a.jpg",
		"/storage/../../etc/passwd",
		"/storage/missing.png",
	} {
		if w := svc.ProbeImageVariants(t.Context(), url); w != nil {
			t.Errorf("URL %q 应返回 nil，实际 %v", url, w)
		}
	}
}
