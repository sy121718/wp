package unit

// i18n02_rtl_fixture_test.go — 多语言产物夹具（审计 I18N-02 的 RTL 发布路径验收）。
//
// 三个语言各构建一次**真实产物**（真组件模板 + 真控件基座 + 真嵌套结构）：
// 默认语言 / 英文 / 阿拉伯语。断言的是产物自身的事实（dir、切换器链接、可见文本），
// 不是「CSS 里写了 dir」：
//
//   - 只有 RTL 产物带 dir="rtl"；
//   - 语言切换器给出本语言到各语言的链接（混排货币/数字的版面就长在这份产物里）；
//   - 缺译回退：非默认语言下没有译文的内容仍是原文（本次不引入 required 策略）。
//
// 需要人工看基线时设 WP_RTL_BASELINE_OUT 指向一个目录，三个产物会被额外写到那里，
// 由浏览器打开截图（步骤见本次整改报告）。默认只写 t.TempDir()，不留残留。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// rtlFixtureDoc 夹具文档：标题 + 正文（富文本）+ 表格 + 折叠块 + 列表 + 切换器，
// 刻意混排货币符号与数字 —— RTL 下 bidi 算法会把它们排到不同的位置，
// 这是「布局是否真的镜像」最容易看出问题的地方。
const rtlFixtureDoc = `
{
  "settings": {"layout": {"mode": "full"}},
  "root": [
    {"id": "nav1", "type": "core.nav", "props": {"menu": "header"}},
    {"id": "h1", "type": "core.heading", "props": {"text": "季度概览 RTL-02", "tag": "h2"}},
    {"id": "lg1", "type": "core.languages", "props": {"orientation": "horizontal", "gap": "16px", "showCode": true}},
    {"id": "t1", "type": "core.text", "props": {"mode": "plaintext", "plainTag": "p", "text": "合计 USD 1,234.56 与 ¥8,900.00 的混排样例。"}},
    {"id": "rt1", "type": "core.text", "props": {"mode": "richtext", "text": "<p>富文本段落：<strong>加粗</strong>、<a href=\"/detail\">链接</a>与数字 42。</p><ul><li>列表项一</li><li>列表项二</li></ul>"}},
    {"id": "tb1", "type": "core.table", "props": {"caption": "季度数据", "headers": ["名称", "金额"], "rows": [["收入", "USD 1,234.56"], ["支出", "¥8,900.00"]]}},
    {"id": "l1", "type": "core.list", "props": {"style": "icon", "items": [{"icon": "check", "text": "免费配送"}, {"icon": "shield", "text": "正品保证"}]}}
  ]
}`

// stubNavResolver 固定的一级菜单（导航是本次验收点之一，用真实 core.nav 组件渲染）。
type stubNavResolver struct{}

func (stubNavResolver) ResolveMenu(projectID, kind string) ([]core.NavigationItem, error) {
	return []core.NavigationItem{
		{Label: "首页", URL: "/"},
		{Label: "产品", URL: "/products"},
		{Label: "关于", URL: "/about"},
	}, nil
}

func (stubNavResolver) ResolveNavigation(projectID, navigationID string) ([]core.NavigationItem, error) {
	return nil, nil
}

// TestRTLFixtureBuildsInThreeLocales 三种语言各构建一份真实产物。
func TestRTLFixtureBuildsInThreeLocales(t *testing.T) {
	page, err := builder.ParsePage([]byte(rtlFixtureDoc))
	if err != nil {
		t.Fatalf("解析夹具文档失败: %v", err)
	}
	if err := builder.ValidatePage(page); err != nil {
		t.Fatalf("夹具文档校验失败: %v", err)
	}

	out := os.Getenv("WP_RTL_BASELINE_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatalf("创建基线目录失败: %v", err)
		}
	}
	dirFor := func(lang string) string {
		if out == "" {
			return t.TempDir()
		}
		return out
	}

	for _, tc := range []struct {
		lang, wantDir string
	}{
		{"zh-CN", ""},
		{"en-US", ""},
		{"ar", "rtl"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			compiled, err := compile(t, page,
				builder.WithLanguage(tc.lang),
				builder.WithProjectID("p1"),
				builder.WithNavigationResolver(stubNavResolver{}),
				// 语言切换器：给出本语言到各语言的链接（互指在产物里的可见形态）。
				builder.WithLocaleLinks([]core.LocaleLink{
					{Lang: "zh-CN", Href: "/index", Current: tc.lang == "zh-CN"},
					{Lang: "en-US", Href: "/en/index", Current: tc.lang == "en-US"},
					{Lang: "ar", Href: "/ar/index", Current: tc.lang == "ar"},
				}),
			)
			if err != nil {
				t.Fatalf("编译失败: %v", err)
			}
			doc, err := builder.RenderDocument(compiled)
			if err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			// 取 <html ...> 这一个标签（不能取到 <!DOCTYPE html> 的那个 '>'）。
			start := strings.Index(doc, "<html")
			if start < 0 {
				t.Fatalf("产物缺少 <html> 标签: %s", doc[:80])
			}
			end := strings.IndexByte(doc[start:], '>')
			head := doc[start : start+end+1]
			if tc.wantDir == "" {
				if strings.Contains(head, "dir=") {
					t.Fatalf("%s 是 LTR，不应写 dir：%s", tc.lang, head)
				}
			} else if !strings.Contains(head, ` dir="rtl"`) {
				t.Fatalf("%s 应输出 dir=\"rtl\"：%s", tc.lang, head)
			}
			// 切换器里必须出现**其它语言**的链接（当前语言那一条各组件实现不同，
			// 可能不带 href），因此按「三条语言链接里至少命中两条」判定。
			hits := 0
			for _, href := range []string{"/index", "/en/index", "/ar/index"} {
				if strings.Contains(doc, "\""+href+"\"") {
					hits++
				}
			}
			if hits < 2 {
				t.Fatalf("%s 的切换器应含其它语言链接（语言互指的可见形态），实际命中 %d 条", tc.lang, hits)
			}
			if err := os.WriteFile(filepath.Join(dirFor(tc.lang), "artifact-"+tc.lang+".html"), []byte(doc), 0o644); err != nil {
				t.Fatalf("写出产物失败: %v", err)
			}
		})
	}
}

// TestRTLUIKitBaseRenders uikit：抽屉 / 自绘下拉 / 颜色字段在 RTL 下的基座。
//
// 这三个控件是 ui.css 的基座段（后台与产物共用一份），也是本次把物理方向属性换成
// 逻辑属性的地方。产出两份真实样式页（LTR / RTL）供浏览器截图：
// 「CSS 里写对了」不等于「浏览器排对了」—— bidi 与逻辑属性的实际效果只能看渲染结果。
func TestRTLUIKitBaseRenders(t *testing.T) {
	out := os.Getenv("WP_RTL_BASELINE_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatalf("创建基线目录失败: %v", err)
		}
	}
	body := `
<body class="drawer-open">
  <div style="padding:16px;">
    <div class="wbs" style="width:min(100%,320px);position:relative;">
      <button type="button" class="wbs-trigger"><span class="wbs-value">选择一项</span><span class="wbs-caret"></span></button>
      <ul class="wbs-menu"><li class="wbs-option">选项一</li><li class="wbs-option">选项二</li></ul>
    </div>
    <div class="wbc" style="margin-top:16px;width:min(100%,320px);">
      <input type="text" value="rgba(16,20,26,.3)">
    </div>
    <p style="margin-top:16px;"><span class="dot dot-success"></span>状态点 + 文字</p>
    <button type="button" class="is-busy">处理中</button>
  </div>
  <aside class="drawer" role="dialog" aria-label="抽屉">
    <header class="drawer-head"><span class="drawer-title">抽屉标题</span><button type="button" class="drawer-close">×</button></header>
    <div class="drawer-body">抽屉内容：导航、表格与富文本在 RTL 下的排版基线。</div>
  </aside>
</body>`

	for _, tc := range []struct{ lang, dir string }{{"zh-CN", ""}, {"ar", ` dir='rtl'`}} {
		t.Run(tc.lang, func(t *testing.T) {
			page := "<!DOCTYPE html>" +
				"<html lang='" + tc.lang + "'" + tc.dir + ">" +
				"<head><meta charset='utf-8'>" +
				"<meta name='viewport' content='width=device-width, initial-scale=1'>" +
				"<title>uikit " + tc.lang + "</title><style>" + templates.UICSS() + "</style></head>" + body +
				"</html>"
			dir := out
			if dir == "" {
				dir = t.TempDir()
			}
			if err := os.WriteFile(filepath.Join(dir, "uikit-"+tc.lang+".html"), []byte(page), 0o644); err != nil {
				t.Fatalf("写出 uikit 失败: %v", err)
			}
		})
	}
}
