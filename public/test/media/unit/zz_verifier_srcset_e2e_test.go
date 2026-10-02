package unit

// zz_verifier_srcset_e2e_test.go — 独立验证者（verifier）的端到端落盘验证。
//
// 复核对象：契约改动（ProbeImageVariants 从 []int 改为 []VariantRef）之后，
// srcset 拼出的 URL 是否**真的指向磁盘上存在的文件**。
//
// 为什么这是最关键的一条：构建期只做字符串拼装，拼错了没有任何构建错误 ——
// 浏览器只会静默回退到 src 原图，没有控制台报错、没有测试失败。
// 所以判据必须是「URL → 存储键 → 磁盘文件存在 → 宽度与像素一致」的完整闭环。

import (
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/builder"
	mediacontract "go_wp/internal/module/media/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/upload"
)

// decodeRealWidth 读磁盘图片的真实像素宽度；读不到返回 -1。
func decodeRealWidth(abs string) int {
	f, err := os.Open(abs)
	if err != nil {
		return -1
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return -1
	}
	return cfg.Width
}

// TestVerifierSrcsetURLsResolveToRealFiles probe → 磁盘闭环（宽度描述符也校验）。
func TestVerifierSrcsetURLsResolveToRealFiles(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "photo.png", 1600, 900)
	id := seedVariantAttachment(t, db, "photo.png", "photo.png")
	ctx := t.Context()

	if _, err := svc.GenerateVariants(ctx, id); err != nil {
		t.Fatalf("GenerateVariants 失败: %v", err)
	}
	refs := svc.ProbeImageVariants(ctx, "/storage/photo.png")
	if len(refs) != 3 {
		t.Fatalf("候选数 = %d（%+v），期望 3（thumb 320 + small 768 + medium 1280；full 与源图同尺寸不参与）", len(refs), refs)
	}

	// DB 里的 file_path 是「下载打包」与「探针 URL」的共同真源，必须完全一致。
	dbKeys := map[string]bool{}
	for _, v := range listVariants(t, db, id) {
		dbKeys[v.FilePath] = true
	}

	for i, r := range refs {
		key := upload.StorageKey(r.URL)
		if key == "" {
			t.Fatalf("候选 %d URL 取不出存储键: %q", i, r.URL)
		}
		abs := filepath.Join(tmp, filepath.FromSlash(key))
		fi, err := os.Stat(abs)
		if err != nil {
			t.Errorf("候选 %d 的 URL 指向不存在的文件: url=%q key=%q abs=%q err=%v",
				i, r.URL, key, abs, err)
			continue
		}
		if fi.Size() == 0 {
			t.Errorf("候选 %d 的文件为空: %q", i, abs)
		}
		if !dbKeys[key] {
			t.Errorf("候选 %d 的 key %q 不在 DB file_path 中（下载打包会与 srcset 指向不同文件）", i, key)
		}
		realW := decodeRealWidth(abs)
		if realW != r.Width {
			t.Errorf("候选 %d 宽度描述符失真: probe=%d 实际=%d (%s)", i, r.Width, realW, key)
		}
		t.Logf("候选 %d: url=%s width=%d 真实文件 %d bytes 实际宽 %d", i, r.URL, r.Width, fi.Size(), realW)
	}
}

// TestVerifierSrcsetEndToEndThroughBuilder 真端到端：
// 真实 probe 注入真实编译内核，从产物 HTML 里抠出 srcset，再逐个落盘校验。
func TestVerifierSrcsetEndToEndThroughBuilder(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "photo.png", 1600, 900)
	id := seedVariantAttachment(t, db, "photo.png", "photo.png")
	ctx := t.Context()
	if _, err := svc.GenerateVariants(ctx, id); err != nil {
		t.Fatalf("GenerateVariants 失败: %v", err)
	}

	set, _ := templates.NewEmbeddedComponentSet()
	// 真实装配路径的形态：pipeline 把 func(ctx,url) []VariantRef 包成 func(url) []VariantRef。
	probe := func(url string) []mediacontract.VariantRef {
		return svc.ProbeImageVariants(ctx, url)
	}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"src":"/storage/photo.png","alt":"A"}}]}`
	page, err := builder.ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析页面失败: %v", err)
	}
	c, err := builder.Compile(page,
		builder.WithComponentSet(set),
		builder.WithThemeSettings(&builder.ThemeSettings{Images: builder.ThemeImages{LazyLoad: "on"}}),
		builder.WithAssetProbe(probe),
	)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	m := regexp.MustCompile(`srcset="([^"]+)"`).FindStringSubmatch(c.HTML)
	if m == nil {
		t.Fatalf("产物里没有 srcset —— 探测结果没进编译内核\n%s", c.HTML)
	}
	srcset := m[1]
	t.Logf("产物 srcset = %s", srcset)

	parts := strings.Split(srcset, ",")
	if len(parts) != 3 {
		t.Fatalf("srcset 候选数 = %d，期望 3（thumb + small + medium）：%q", len(parts), srcset)
	}
	for _, p := range parts {
		fields := strings.Fields(strings.TrimSpace(p))
		if len(fields) != 2 || !strings.HasSuffix(fields[1], "w") {
			t.Fatalf("srcset 片段格式异常: %q", p)
		}
		url := fields[0]
		key := upload.StorageKey(url)
		abs := filepath.Join(tmp, filepath.FromSlash(key))
		fi, err := os.Stat(abs)
		if err != nil {
			t.Errorf("srcset 里的 URL 指向不存在的文件: %q → %q (%v)", url, abs, err)
			continue
		}
		realW := decodeRealWidth(abs)
		dw, cerr := strconv.Atoi(strings.TrimSuffix(fields[1], "w"))
		if cerr == nil && realW >= 0 && dw != realW {
			t.Errorf("srcset 宽度声明失真: %s 声明 %dw 实际 %dpx", url, dw, realW)
		}
		t.Logf("srcset 候选 %s → 落盘 %s（%d bytes，实际宽 %d）", url, key, fi.Size(), realW)
	}
}

// TestVerifierFingerprintTracksContent 换图后候选 URL 必须变化 ——
// 这是 immutable 缓存安全的**唯一**前提：URL 与内容绑定。
// 若内容变了 URL 不变，immutable 就等于「老访客永久看不到新图」。
func TestVerifierFingerprintTracksContent(t *testing.T) {
	db, svc := newMediaUnitService(t)
	tmp := initVariantUploadForTest(t)
	makeTestPNG(t, tmp, "photo.png", 1600, 900)
	id := seedVariantAttachment(t, db, "photo.png", "photo.png")
	ctx := t.Context()

	if _, err := svc.GenerateVariants(ctx, id); err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	before := svc.ProbeImageVariants(ctx, "/storage/photo.png")
	if len(before) == 0 {
		t.Fatal("首次 probe 为空")
	}

	// 内容不变重跑：URL 必须完全一致（确定性构建的前提）。
	if _, err := svc.GenerateVariants(ctx, id); err != nil {
		t.Fatalf("重跑失败: %v", err)
	}
	same := svc.ProbeImageVariants(ctx, "/storage/photo.png")
	if len(same) != len(before) {
		t.Fatalf("重跑后候选数变了: %d → %d", len(before), len(same))
	}
	for i := range before {
		if before[i].URL != same[i].URL {
			t.Errorf("内容未变但 URL 变了：%q → %q（确定性被破坏）", before[i].URL, same[i].URL)
		}
	}

	// 换图：改字节 + generation+1（复现 media_replace 的语义：文件名不变、字节变）。
	makeTestPNG(t, tmp, "photo.png", 1200, 700)
	if err := db.Exec("UPDATE sys_attachment SET generation = generation + 1 WHERE id = ?", id).Error; err != nil {
		t.Fatalf("更新 generation 失败: %v", err)
	}
	if _, err := svc.GenerateVariants(ctx, id); err != nil {
		t.Fatalf("换图后生成失败: %v", err)
	}
	after := svc.ProbeImageVariants(ctx, "/storage/photo.png")
	if len(after) == 0 {
		t.Fatal("换图后 probe 为空")
	}

	oldURLs := map[string]bool{}
	for _, r := range before {
		oldURLs[r.URL] = true
	}
	for _, r := range after {
		t.Logf("换图后候选: %s (%dw)", r.URL, r.Width)
		if oldURLs[r.URL] {
			t.Errorf("换图后 URL 未变: %q —— immutable 缓存会让老访客永久看到旧图", r.URL)
		}
	}
}
