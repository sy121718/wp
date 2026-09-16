package unit

// example_tiers_test.go — 示例插件三档验收（审计 VIS-015）。
//
// 覆盖两件事，两件都走真实链路，不是伪造：
//  1. 安装：examples/ 下的目录 → zip → Service.Install（安全解包 + manifest 白名单校验
//     + L1 迁移）→ EnabledAssembly 组件规格 / 预设 / schema 版本就位；
//  2. 渲染：EnabledAssembly 的 Specs + PluginFS + ExtraCSS 喂给 builder.Compile，
//     按页面文档真编译出 HTML + 分层 CSS（插件静态资产在 @layer sky-plugin）。
//
// 档位分工：l0-style-only 纯样式声明；l0-template-assets 模板片段 + 静态资产；
// l1-marketing L1 数据层 + 容器/主题查询。

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	plugincontract "go_wp/internal/module/plugin/contract"
	"go_wp/internal/templates"
)

// exampleTier 一档示例插件的验收清单。
type exampleTier struct {
	dir        string   // examples/ 下的目录名
	id         string   // manifest.id
	components []string // 期望就位的组件类型
	presets    []string // 期望就位的预设 ID
	schemaVer  int      // 期望登记的 L1 schema 版本
	wantCSS    []string // 编译产物 CSS 必须包含的片段
	wantHTML   []string // 编译产物 HTML 必须包含的片段
	page       string   // 渲染用页面文档
}

// exampleTiers 三档示例（与 examples/ 目录一一对应）。
var exampleTiers = []exampleTier{
	{
		dir:        "l0-style-only",
		id:         "styletile",
		components: []string{"plugin.styletile.styletile_note"},
		schemaVer:  0,
		wantCSS: []string{
			// 伪类分派：悬浮（触屏不输出）/ 触屏等价形态 / 按压反馈
			"@media (hover: hover) {",
			"@media (hover: none) {",
			"var(--st-accent)",
			"border-left-color: #16a34a", // when tone=success
			":first-child {",             // 结构伪类
			"--st-accent: #16a34a",       // vars 导出（accent 控件）
			"padding: 20px",              // bindings（padY 控件）
			"@container (width >= 480px) {",
			"@layer sky-auto {",
			"@container sky-theme style(--sky-density: compact) {",
			"@layer sky-theme {",
			"@layer sky-base {",
		},
		wantHTML: []string{"st-title", "库存告急", "仅剩 3 件。"},
		page: `{"settings": {"layout": {"mode": "full"}}, "root": [{
		  "id": "note-1",
		  "type": "plugin.styletile.styletile_note",
		  "props": {"title": "库存告急", "body": "仅剩 3 件。", "tone": "success",
		            "accent": "#16a34a", "padY": "20px", "lift": "-3px"}
		}]}`,
	},
	{
		dir:        "l0-template-assets",
		id:         "notecards",
		components: []string{"plugin.notecards.notecards_card", "plugin.notecards.notecards_badge"},
		presets:    []string{"notecards-feature"},
		schemaVer:  0,
		wantCSS: []string{
			"@layer sky-plugin {",  // assets/card.css 静态资产层
			".nc-card__subtitle {", // 静态资产内的选择器
			"--nc-accent: #7c3aed", // 变量导出
			"border-radius: 20px",  // bindings（radius 控件）
			"text-align: center",   // when align=center
			"@media (hover: hover) {",
		},
		wantHTML: []string{"nc-card__price", "免费", "nc-badge", "NEW"},
		page: `{"settings": {"layout": {"mode": "full"}}, "root": [
		  {"id": "card-1", "type": "plugin.notecards.notecards_card", "props": {
		    "title": "快速上线", "subtitle": "Starter", "body": "预设插入即可发布。",
		    "price": "免费", "accent": "#7c3aed", "radius": "20px", "align": "center"}},
		  {"id": "card-2", "type": "plugin.notecards.notecards_card", "props": {
		    "title": "仅标题", "subtitle": "", "price": ""}},
		  {"id": "badge-1", "type": "plugin.notecards.notecards_badge", "props": {
		    "label": "NEW", "accent": "#dc2626"}}
		]}`,
	},
	{
		dir:        "l1-marketing",
		id:         "campaignkit",
		components: []string{"plugin.campaignkit.campaignkit_hero", "plugin.campaignkit.campaignkit_coupon"},
		presets:    []string{"campaignkit-hero-preset"},
		schemaVer:  1,
		wantCSS: []string{
			"@container (width >= 640px) {",
			"@layer sky-auto {",
			"@container sky-theme style(--sky-density: compact) {",
			"@layer sky-theme {",
			"@container sky-ink style(--sky-scheme: light) {",
			"@layer sky-local {",
			"--ck-accent: #f97316",
			"var(--ck-accent-soft, #ea580c)",
			"@media (hover: none) {",
			"@layer sky-plugin {", // assets/hero.css
		},
		wantHTML: []string{"立即预定", "/site/presale", "PRESALE80", "满 299 减 80"},
		page: `{"settings": {"layout": {"mode": "full"}}, "root": [
		  {"id": "hero-1", "type": "plugin.campaignkit.campaignkit_hero", "props": {
		    "title": "双十一预售", "subtitle": "定金膨胀 10 倍。", "ctaLabel": "立即预定",
		    "ctaHref": "/site/presale", "accent": "#f97316", "accentSoft": "#ea580c",
		    "align": "center"}},
		  {"id": "coupon-1", "type": "plugin.campaignkit.campaignkit_coupon", "props": {
		    "benefit": "满 299 减 80", "code": "PRESALE80", "expires": "有效期至 11 月 11 日",
		    "accent": "#db2777"}}
		]}`,
	},
}

// specResolver 用装配产出的组件规格实现 builder 的插件解析契约。
type specResolver map[string]*core.PluginComponentSpec

func (m specResolver) LookupPluginComponent(typeName string) (*core.PluginComponentSpec, bool) {
	s, ok := m[typeName]
	return s, ok
}

// exampleFiles 读取示例插件目录（README 属人读文档，不进包）。
func exampleFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	root := filepath.Join(repoRoot(t), "examples", dir)
	files := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if filepath.Base(rel) == "README.md" {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		files[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历示例目录 %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("示例目录 %s 为空", dir)
	}
	return files
}

// zipExampleBundle 把示例文件打成 zip（与文档里的 `zip -r` 同形状）。
func zipExampleBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip Create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip Write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

// compileWithAssembly 用真实装配素材编译一段页面文档。
// 返回三个值：整篇文档（含 <style>）、组件树 HTML（不含 CSS，用于断言结构）、产物 CSS。
// 断言结构必须用组件树 HTML —— 整篇文档里带 <style>，CSS 选择器会造成假匹配。
func compileWithAssembly(t *testing.T, asm *plugincontract.Assembly, pageJSON string) (doc, body, css string) {
	t.Helper()
	set, err := templates.NewCompositeSet(asm.PluginFS)
	if err != nil {
		t.Fatalf("CompositeSet: %v", err)
	}
	page, err := builder.ParsePage([]byte(pageJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if err = builder.ValidatePage(page); err != nil {
		t.Fatalf("ValidatePage: %v", err)
	}
	compiled, err := builder.Compile(page,
		builder.WithComponentSet(set),
		builder.WithPluginResolver(specResolver(asm.Specs)),
		builder.WithExtraCSS(strings.Join(asm.ExtraCSS, "\n\n")),
	)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	out, err := builder.RenderDocument(compiled)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	return out, compiled.HTML, compiled.CSS
}

// TestExampleTiersEndToEnd 三档示例：安装（含 L1 迁移）→ 装配 → 渲染。
func TestExampleTiersEndToEnd(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	for _, tier := range exampleTiers {
		zipBytes := zipExampleBundle(t, exampleFiles(t, tier.dir))
		if _, err := svc.Install(ctx, zipBytes); err != nil {
			t.Fatalf("[%s] 安装失败（按 README 的 zip 步骤应可安装）: %v", tier.dir, err)
		}
	}

	asm, err := svc.EnabledAssembly(ctx)
	if err != nil {
		t.Fatalf("EnabledAssembly: %v", err)
	}
	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]int{}
	for _, row := range list {
		byID[row.ID] = row.SchemaVersion
	}

	for _, tier := range exampleTiers {
		t.Run(tier.dir, func(t *testing.T) {
			for _, comp := range tier.components {
				if _, ok := asm.Specs[comp]; !ok {
					t.Fatalf("组件规格未就位: %s", comp)
				}
			}
			got := map[string]bool{}
			for _, p := range asm.Presets {
				got[p.ID] = true
			}
			for _, want := range tier.presets {
				if !got[want] {
					t.Fatalf("预设未就位: %s", want)
				}
			}
			if v, ok := byID[tier.id]; !ok || v != tier.schemaVer {
				t.Fatalf("registry 记账不符: id=%s schemaVersion=%d（期望 %d）", tier.id, v, tier.schemaVer)
			}

			_, html, css := compileWithAssembly(t, asm, tier.page)
			for _, want := range tier.wantCSS {
				if !strings.Contains(css, want) {
					t.Fatalf("产物 CSS 缺少 %q\n---- CSS ----\n%s", want, css)
				}
			}
			for _, want := range tier.wantHTML {
				if !strings.Contains(html, want) {
					t.Fatalf("产物 HTML 缺少 %q\n---- HTML ----\n%s", want, html)
				}
			}
		})
	}
}

// TestExampleTemplateConditional 模板条件输出：控件为空时整块不渲染。
func TestExampleTemplateConditional(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	zipBytes := zipExampleBundle(t, exampleFiles(t, "l0-template-assets"))
	if _, err := svc.Install(ctx, zipBytes); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	asm, err := svc.EnabledAssembly(ctx)
	if err != nil {
		t.Fatalf("EnabledAssembly: %v", err)
	}
	// card-1 传了副标题：渲染出来；card-2 显式传空串：整块不输出。
	_, html, _ := compileWithAssembly(t, asm, `{"settings": {"layout": {"mode": "full"}}, "root": [
	  {"id": "card-1", "type": "plugin.notecards.notecards_card", "props": {"title": "A", "subtitle": "Starter"}},
	  {"id": "card-2", "type": "plugin.notecards.notecards_card", "props": {"title": "B", "subtitle": "", "price": ""}}
	]}`)
	if !strings.Contains(html, "nc-card__subtitle") {
		t.Fatalf("card-1 应渲染副标题:\n%s", html)
	}
	first := strings.Index(html, "sky-c-card-1")
	second := strings.Index(html, "sky-c-card-2")
	if first < 0 || second < 0 || second < first {
		t.Fatalf("两卡片节点的位置异常:\n%s", html)
	}
	if strings.Contains(html[second:], "nc-card__subtitle") {
		t.Fatalf("card-2 副标题为空，{if} 分支应不输出:\n%s", html[second:])
	}
	if strings.Contains(html[second:], "nc-card__price") {
		t.Fatalf("card-2 价格为空，{if} 分支应不输出:\n%s", html[second:])
	}
}

// TestExamplePresetsCompile 预设文档可编译：把 presets.document 当页面 root 编译
// （等价于「工作台插入预设后发布」这一步）。
func TestExamplePresetsCompile(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	for _, tier := range exampleTiers {
		if len(tier.presets) == 0 {
			continue // 纯样式档没有预设
		}
		zipBytes := zipExampleBundle(t, exampleFiles(t, tier.dir))
		if _, err := svc.Install(ctx, zipBytes); err != nil {
			t.Fatalf("[%s] 安装失败: %v", tier.dir, err)
		}
	}
	asm, err := svc.EnabledAssembly(ctx)
	if err != nil {
		t.Fatalf("EnabledAssembly: %v", err)
	}
	checked := 0
	for _, preset := range asm.Presets {
		pageJSON := `{"settings": {"layout": {"mode": "full"}}, "root": ` + string(preset.Document) + `}`
		_, html, _ := compileWithAssembly(t, asm, pageJSON)
		if strings.TrimSpace(html) == "" {
			t.Fatalf("预设 %s 编译产物为空", preset.ID)
		}
		checked++
	}
	if checked < 2 {
		t.Fatalf("应至少编译两个预设（notecards / campaignkit），实际 %d", checked)
	}
}
