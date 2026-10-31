package unit

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

type exampleTier struct {
	dir        string
	id         string
	components []string
	presets    []string
	schemaVer  int
	wantCSS    []string
	wantHTML   []string
	page       string
}

var exampleTiers = []exampleTier{
	{
		dir:        "l0-style-only",
		id:         "styletile",
		components: []string{"plugin.styletile.styletile_note"},
		schemaVer:  0,
		wantCSS: []string{

			"@media (hover: hover) {",
			"@media (hover: none) {",
			"var(--st-accent)",
			"border-left-color: #16a34a",
			":first-child {",
			"--st-accent: #16a34a",
			"padding: 20px",
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
			"@layer sky-plugin {",
			".nc-card__subtitle {",
			"--nc-accent: #7c3aed",
			"border-radius: 20px",
			"text-align: center",
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
			"@layer sky-plugin {",
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

type specResolver map[string]*core.PluginComponentSpec

func (m specResolver) LookupPluginComponent(typeName string) (*core.PluginComponentSpec, bool) {
	s, ok := m[typeName]
	return s, ok
}

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

func TestExamplePresetsCompile(t *testing.T) {
	svc := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()
	for _, tier := range exampleTiers {
		if len(tier.presets) == 0 {
			continue
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
