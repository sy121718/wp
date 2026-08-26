// Package unit 媒体中心（docs/02-B）领域内核的单元测试。
package unit

import (
	"hash/fnv"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/builder/media"
)

// hash64 由种子生成 64 位十六进制测试哈希（满足媒体域 hex 白名单）。
func hash64(seed string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(seed))
	sum := h.Sum64()
	// 循环扩展为 64 个十六进制字符。
	out := make([]byte, 64)
	for i := range out {
		out[i] = "0123456789abcdef"[(sum>>uint(4*(i%16)))&0xF]
	}
	return string(out)
}

// seedImageAsset 构造带完整变体集合的图片资产。
func seedImageAsset(t *testing.T, s *media.Store, hash, alt string) string {
	t.Helper()
	id, dup, err := s.Upload(media.Asset{
		Hash:     hash,
		FileName: "hero.jpg",
		MimeType: "image/jpeg",
		Type:     media.TypeImage,
		Width:    1920,
		Height:   1080,
		Size:     1024,
		Alt:      alt,
		Title:    "全局标题",
		CategoryID: "brand",
		Tags:     []string{"品牌素材"},
		Variants: []media.Variant{
			{Kind: media.VariantThumbnail, Format: "webp", URL: "/m/hero-320.webp", Width: 320, Height: 180},
			{Kind: media.VariantMedium, Format: "webp", URL: "/m/hero-768.webp", Width: 768, Height: 432},
			{Kind: media.VariantLarge, Format: "webp", URL: "/m/hero-1280.webp", Width: 1280, Height: 720},
			{Kind: media.VariantOriginal, Format: "jpeg", URL: "/m/hero.jpg", Width: 1920, Height: 1080},
			{Kind: media.VariantThumbnail, Format: "avif", URL: "/m/hero-320.avif", Width: 320, Height: 180},
		},
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if dup != "" {
		t.Fatalf("首次上传即报重复: %s", dup)
	}
	return id
}

// TestUploadDeduplicate 上传去重：同哈希返回重复标记，assetId 稳定派生。
func TestUploadDeduplicate(t *testing.T) {
	s := media.NewStore()
	h := hash64("aaa")
	id1 := seedImageAsset(t, s, h, "品牌主图")

	id2, dup, err := s.Upload(media.Asset{Hash: h, FileName: "dup.jpg", Type: media.TypeImage, Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("重复上传报错: %v", err)
	}
	if dup == "" {
		t.Error("同哈希上传未检测到重复")
	}
	if id1 != id2 {
		t.Errorf("同一内容的 assetId 不稳定: %s vs %s", id1, id2)
	}
	if !strings.HasPrefix(id1, "ast_") {
		t.Errorf("assetId 未按稳定格式派生: %s", id1)
	}
}

// TestUploadValidation 上传校验：非法哈希/类型/宽高拒绝。
func TestUploadValidation(t *testing.T) {
	s := media.NewStore()
	cases := []struct {
		name  string
		asset media.Asset
	}{
		{"非法哈希", media.Asset{Hash: "xyz", Type: media.TypeImage, Width: 1, Height: 1}},
		{"非法类型", media.Asset{Hash: hash64("a"), Type: "audio", Width: 1, Height: 1}},
		{"图片缺宽高", media.Asset{Hash: hash64("b"), Type: media.TypeImage}},
		{"非法标签", media.Asset{Hash: hash64("c"), Type: media.TypeDocument, Tags: []string{"a b!"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := s.Upload(tc.asset); err == nil {
				t.Error("期望校验失败，实际通过")
			}
		})
	}
}

// TestReplace 版本替换：assetId 与引用关系保留，Generation 递增。
func TestReplace(t *testing.T) {
	s := media.NewStore()
	id := seedImageAsset(t, s, hash64("old"), "旧图")
	if err := s.RecordRef(id, media.Reference{Kind: "page", ID: "p1", Title: "首页"}); err != nil {
		t.Fatalf("登记引用失败: %v", err)
	}

	newHash := hash64("new")
	if err := s.Replace(id, newHash, 800, 600, 2048, nil); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	a, _ := s.Get(id)
	if a.Hash != newHash || a.Generation != 2 || a.Width != 800 {
		t.Errorf("替换后元数据异常: hash=%s gen=%d w=%d", a.Hash, a.Generation, a.Width)
	}
	if refs := s.Refs(id); len(refs) != 1 {
		t.Errorf("替换丢失引用关系，剩 %d 条", len(refs))
	}
	// 同内容重复替换幂等。
	if err := s.Replace(id, newHash, 800, 600, 2048, nil); err != nil {
		t.Fatalf("幂等替换失败: %v", err)
	}
	if a, _ := s.Get(id); a.Generation != 2 {
		t.Errorf("幂等替换不应递增 Generation: %d", a.Generation)
	}
}

// TestDeleteReferenceProtection 引用保护：被引用资产删除拦截并列出引用清单。
func TestDeleteReferenceProtection(t *testing.T) {
	s := media.NewStore()
	id := seedImageAsset(t, s, hash64("pp"), "受保护图")

	if err := s.Delete(id); err != nil {
		t.Fatalf("未引用资产应可删除: %v", err)
	}

	id2 := seedImageAsset(t, s, hash64("qq"), "受保护图2")
	_ = s.RecordRef(id2, media.Reference{Kind: "article", ID: "a9", Title: "新品发布"})
	_ = s.RecordRef(id2, media.Reference{Kind: "page", ID: "p1", Title: "首页"})
	_ = s.RecordRef(id2, media.Reference{Kind: "article", ID: "a9"}) // 幂等（无 Title，不覆盖已有标题）

	err := s.Delete(id2)
	if err == nil {
		t.Fatal("被引用资产删除未被拦截")
	}
	if !strings.Contains(err.Error(), "新品发布") || !strings.Contains(err.Error(), "首页") {
		t.Errorf("拦截警告未包含引用清单: %v", err)
	}
	if !strings.Contains(err.Error(), "2 处引用") {
		t.Errorf("引用计数异常: %v", err)
	}

	// 移除引用后可删除。
	s.RemoveRef(id2, media.Reference{Kind: "article", ID: "a9"})
	s.RemoveRef(id2, media.Reference{Kind: "page", ID: "p1"})
	if err := s.Delete(id2); err != nil {
		t.Errorf("引用清除后删除仍被拦截: %v", err)
	}
}

// TestSearch 多维检索：文件名/类型/分类/标签/引用状态。
func TestSearch(t *testing.T) {
	s := media.NewStore()
	id1 := seedImageAsset(t, s, hash64("s1"), "图一")
	_ = s.RecordRef(id1, media.Reference{Kind: "page", ID: "p1"})

	id2, _, err := s.Upload(media.Asset{
		Hash: hash64("s2"), FileName: "spec.pdf", MimeType: "application/pdf",
		Type: media.TypeDocument, Size: 100, CategoryID: "docs",
	})
	if err != nil {
		t.Fatalf("上传文档失败: %v", err)
	}

	if got := s.Search(media.SearchFilter{FileName: "hero"}); len(got) != 1 || got[0].ID != id1 {
		t.Errorf("按文件名检索异常: %d 条", len(got))
	}
	if got := s.Search(media.SearchFilter{Type: media.TypeDocument}); len(got) != 1 || got[0].ID != id2 {
		t.Errorf("按类型检索异常: %d 条", len(got))
	}
	if got := s.Search(media.SearchFilter{Tag: "品牌素材"}); len(got) != 1 {
		t.Errorf("按标签检索异常: %d 条", len(got))
	}
	refTrue := true
	if got := s.Search(media.SearchFilter{Referenced: &refTrue}); len(got) != 1 || got[0].ID != id1 {
		t.Errorf("按引用状态检索异常: %d 条", len(got))
	}
	refFalse := false
	if got := s.Search(media.SearchFilter{Referenced: &refFalse}); len(got) != 1 || got[0].ID != id2 {
		t.Errorf("按未引用状态检索异常: %d 条", len(got))
	}
}

// TestResolveMediaVariants 构建期解析：变体选择、srcset、现代格式 source、全局 SEO。
func TestResolveMediaVariants(t *testing.T) {
	s := media.NewStore()
	id := seedImageAsset(t, s, hash64("rv"), "响应式主图")

	meta, err := s.ResolveMedia(id, media.VariantMedium)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if meta.Type != core.MediaTypeImage {
		t.Errorf("类型异常: %s", meta.Type)
	}
	// 期望 medium 规格：精确命中 768 宽。
	if meta.URL != "/m/hero-768.webp" || meta.Width != 768 || meta.Height != 432 {
		t.Errorf("变体选择异常: %s %dx%d", meta.URL, meta.Width, meta.Height)
	}
	if meta.Alt != "响应式主图" || meta.Title != "全局标题" {
		t.Errorf("全局 SEO 元数据丢失: %q %q", meta.Alt, meta.Title)
	}
	// 同格式（webp）srcset 按宽度升序。
	wantSrcset := "/m/hero-320.webp 320w, /m/hero-768.webp 768w, /m/hero-1280.webp 1280w"
	if meta.Srcset != wantSrcset {
		t.Errorf("srcset 异常:\nwant %s\ngot  %s", wantSrcset, meta.Srcset)
	}
	// 现代格式 source：AVIF + WebP 各一条，AVIF 优先声明。
	if len(meta.Sources) != 2 || meta.Sources[0].Type != "image/avif" || meta.Sources[1].Type != "image/webp" {
		t.Errorf("现代格式 source 异常: %+v", meta.Sources)
	}
}

// TestResolveMediaNonImage 非图片资产解析：返回稳定 URL 基础元数据。
func TestResolveMediaNonImage(t *testing.T) {
	s := media.NewStore()
	id, _, err := s.Upload(media.Asset{
		Hash: hash64("pdf"), FileName: "manual.pdf", MimeType: "application/pdf",
		Type: media.TypeDocument, Size: 100, Alt: "产品手册",
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	meta, err := s.ResolveMedia(id, media.VariantOriginal)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if meta.Type != core.MediaTypeDocument || meta.URL == "" || meta.Alt != "产品手册" {
		t.Errorf("文档解析异常: %+v", meta)
	}
	if meta.Srcset != "" || meta.Sources != nil {
		t.Errorf("文档不应携带图片变体: %+v", meta)
	}
}

// TestImageCompilePipeline 数据流闭环：Page Document（仅 assetId）→ 编译 → 响应式 <picture>。
func TestImageCompilePipeline(t *testing.T) {
	s := media.NewStore()
	id := seedImageAsset(t, s, hash64("pipe"), "产品图")

	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pic","type":"core.image","props":{"assetId":"` + id + `","variant":"medium","alt":"局部覆盖","sizes":"50vw"}}]}`
	c, err := builder.Compile(mustParse(t, doc), builder.WithMediaResolver(s))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, want := range []string{
		"<picture>",
		`<source type="image/avif" srcset="/m/hero-320.avif 320w" sizes="50vw">`,
		`<img src="/m/hero-768.webp"`,
		`width="768" height="432"`,
		`loading="lazy"`,
		`srcset="/m/hero-320.webp 320w, /m/hero-768.webp 768w, /m/hero-1280.webp 1280w"`,
		`sizes="50vw"`,
		`alt="局部覆盖"`,   // 局部覆盖全局
		`title="全局标题"`, // 未覆盖时继承
		"</picture>",
	} {
		if !strings.Contains(c.HTML, want) {
			t.Errorf("HTML 缺少 %q\n实际: %s", want, c.HTML)
		}
	}
}

// TestImageMissingResolver 缺少解析器：报明确错误而非静默。
func TestImageMissingResolver(t *testing.T) {
	s := media.NewStore()
	id := seedImageAsset(t, s, hash64("nr"), "无解析器")
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pic","type":"core.image","props":{"assetId":"` + id + `"}}]}`
	_, err := builder.Compile(mustParse(t, doc))
	if err == nil || !strings.Contains(err.Error(), "媒体解析器") {
		t.Errorf("缺少解析器应报明确错误: %v", err)
	}
}

// TestImageWrongAssetType 类型断言：文档资产挂到图片组件必须报错。
func TestImageWrongAssetType(t *testing.T) {
	s := media.NewStore()
	id, _, err := s.Upload(media.Asset{
		Hash: hash64("d2"), FileName: "b.pdf", MimeType: "application/pdf", Type: media.TypeDocument, Size: 1,
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pic","type":"core.image","props":{"assetId":"` + id + `"}}]}`
	_, err = builder.Compile(mustParse(t, doc), builder.WithMediaResolver(s))
	if err == nil || !strings.Contains(err.Error(), "仅支持图片类资产") {
		t.Errorf("类型不匹配应报错: %v", err)
	}
}

// TestImageValidateProps 图片组件校验：非法 assetId/变体/叶子约束。
func TestImageValidateProps(t *testing.T) {
	cases := []struct{ name, json, want string }{
		{"非法assetId", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"assetId":"a b"}}]}`, "无效的 assetId"},
		{"非法变体", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"assetId":"ast_x","variant":"huge"}}]}`, "无效的变体规格"},
		{"图片带子节点", `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"i1","type":"core.image","props":{"assetId":"ast_x"},"children":[{"id":"c1","type":"core.image","props":{"assetId":"ast_y"}}]}]}`, "叶子节点"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builder.Compile(mustParse(t, tc.json))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

// mustParse 解析页面文档（失败即 Fatal）。
func mustParse(t *testing.T, jsonStr string) *builder.Page {
	t.Helper()
	p, err := builder.ParsePage([]byte(jsonStr))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	return p
}