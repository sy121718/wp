package unit

// media_probe_unit_test.go — 构建期图片变体探测（页面 srcset 的数据源）单元测试。
//
// ProbeImageVariants 是「构建期响应式图片」的注入点：page 装配层把它作为
// builder.WithAssetProbe 传入编译内核，只有媒体库且变体已就绪的 URL 才输出 srcset。
import (
	"path"
	"regexp"
	"testing"
)

// variantRefRe 候选 URL 的命名约定，与生产侧同源（storage_cache.go 的 storageImmutableVariant）：
// <词干>_<类型>-<generation>-<hash8>.jpg。词干**不限定为数字**
// —— 存量语义化原名的变体同样带指纹，中间件的 immutable 判定也认它们。
var variantRefRe = regexp.MustCompile(`_(?:thumb|small|medium|full)-\d+-[0-9a-f]{8}\.jpg$`)

// TestProbeImageVariants 只返回已就绪且能映射到变体文件的候选；
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
	// 候选按宽度升序：thumb 320 / small 768 / medium 1280
	// （full 与源图同尺寸，不参与 srcset）。
	wantWidths := []int{320, 768, 1280}
	if len(got) != len(wantWidths) {
		t.Fatalf("候选数 = %d（%v），期望 %d（%v）", len(got), got, len(wantWidths), wantWidths)
	}
	for i, w := range wantWidths {
		if got[i].Width != w {
			t.Fatalf("第 %d 个候选宽度 = %d，期望 %d", i, got[i].Width, w)
		}
		// URL 必须带内容指纹且可直接落到文件：构建期只做字符串拼装，
		// 拼错了没有任何构建错误 —— 浏览器只会静默回退到原图，症状最难查。
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
