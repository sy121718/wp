// jetview_test.go — 组件渲染迁移的字节等价测试（Phase 3 切换验收）。
//
// 切换 Compile 到 Jet 路径后，「旧 Go render 路径」与「新 Jet 路径」不再是两条
// 并行的生产路径，因此原「旧 Compile vs 新 nodeViewOf+renderView」对比不再成立。
// 本文件改为 golden 对比：
//
//   - testdata/golden/ 下的 golden 文件由切换前的旧 render 路径（core.RenderNode）
//     一次性生成并固化，作为「切换前产物」的字节基准；
//   - TestJetViewByteEquivalent 断言新 Compile 产物与 golden 字节一致，
//     证明「Compile 切换到 Jet 路径后，产物与切换前完全一致」。
package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// jetDocJSON 覆盖：顶级 Section（wp-section）、三端 CSS、入场动效关键帧、
// 形状分隔线（top/bottom）、button 外链/内链/图标前后缀/自定义 class 与 ID/特殊字符转义。
const jetDocJSON = `{
  "settings": {
    "layout": {"mode": "full"},
    "base": {},
    "seo": {"title": "Jet 等价测试", "description": "测试页面"}
  },
  "root": [
    {
      "id": "hero",
      "type": "core.container",
      "props": {
        "tag": "section",
        "layout": {"engine": "flex", "flex": {"direction": "column", "justify": "center", "align": "stretch", "gap": "24px"}},
        "box": {"padding": {"desktop": "40px", "tablet": "24px", "mobile": "16px"}},
        "visual": {"bgColor": "#f7f7f8", "radius": "16px", "shadow": "md"},
        "interaction": {"entrance": "fade-in"},
        "styleEx": {"shapeDivider": "wave"}
      },
      "children": [
        {
          "id": "cta",
          "type": "core.button",
          "props": {
            "text": "立即购买 <span> & 优惠",
            "action": "external",
            "value": "https://example.com/buy?x=1&y=2",
            "target": "blank",
            "rel": "nofollow",
            "size": "lg",
            "shape": "pill",
            "variant": "solid",
            "normal": {"background": "#111111", "color": "#ffffff"},
            "hover": {"background": "#333333"},
            "icon": {"source": "builtin", "name": "arrow-right", "position": "suffix", "spacing": "8px"},
            "advanced": {"customClasses": ["my-cta"], "customId": "cta-btn"}
          }
        },
        {
          "id": "inner",
          "type": "core.container",
          "props": {
            "tag": "div",
            "layout": {"engine": "grid", "grid": {"columns": {"desktop": 2, "tablet": 1}, "columnGap": "16px", "rowGap": "16px"}},
            "box": {"padding": {"desktop": "20px"}},
            "visual": {"borderWidth": "1px", "borderStyle": "solid", "borderColor": "#e0e0e0"},
            "styleEx": {"shapeDivider": "slant", "shapeDividerPosition": "top"}
          },
          "children": [
            {
              "id": "ghost",
              "type": "core.button",
              "props": {
                "text": "了解更多",
                "action": "internal",
                "value": "/about-us",
                "variant": "ghost",
                "icon": {"source": "builtin", "name": "arrow-left", "position": "prefix", "hoverShift": "4px"}
              }
            }
          ]
        }
      ]
    }
  ]
}`

// rootDoc 把 root JSON 包装为完整页面文档。
func rootDoc(rootJSON string) string {
	return `{"settings": {"layout": {"mode": "full"}, "seo": {"title": "等价测试"}}, "root": ` + rootJSON + `}`
}

// mockBlockResolver 测试用全局块解析器：块 ID → 块文档 root 节点。
type mockBlockResolver struct {
	roots map[string][]*core.Node
}

func (m *mockBlockResolver) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	roots, ok := m.roots[blockID]
	if !ok {
		return nil, fmt.Errorf("block not found: %s", blockID)
	}
	return roots, nil
}

// goldenCase 单个字节等价用例（name 唯一，对应 golden 文件名）。
type goldenCase struct {
	name  string
	doc   string
	block core.BlockResolver
}

// goldenCases 全部字节等价用例（主用例 + 18 组件 + globalref 展开）。
var goldenCases = []goldenCase{
	{name: "main", doc: jetDocJSON},
	{name: "heading", doc: rootDoc(`[
		{"id":"h1","type":"core.heading","props":{"text":"标题 <b>&</b>","tag":"h2","subtitle":"副标题 & 更多","highlightColor":"#fff000","highlightPadding":"4px 8px","highlightRadius":"6px","advanced":{"customClasses":["my-cls"],"customId":"my-heading"}}},
		{"id":"h2","type":"core.heading","props":{"text":"二级","tag":"h3","weight":"bold","transform":"uppercase","decor":{"decoration":"underline","decorationColor":"#f00"},"color":"#333"}}
	]`)},
	{name: "text", doc: rootDoc(`[
		{"id":"t1","type":"core.text","props":{"mode":"plaintext","plainTag":"p","text":"纯文本 <b>&</b> 特殊"}},
		{"id":"t2","type":"core.text","props":{"mode":"richtext","text":"<p>富文本 <strong>加粗</strong> & 更多</p><script>alert(1)</script>"}}
	]`)},
	{name: "image", doc: rootDoc(`[
		{"id":"img1","type":"core.image","props":{"src":"https://example.com/a.jpg","alt":"图片 & 说明","loading":"eager","fetchPriority":"high"}},
		{"id":"img2","type":"core.image","props":{"src":"https://example.com/b.jpg","alt":"链接图","clickAction":"link","link":"https://example.com/t?x=1&y=2","linkTarget":"blank","linkRel":"nofollow","advanced":{"customId":"img-link"}}},
		{"id":"img3","type":"core.image","props":{"src":"https://example.com/c.jpg","alt":"灯箱","clickAction":"lightbox","caption":"图注 & 内容"}}
	]`)},
	{name: "divider", doc: rootDoc(`[
		{"id":"d1","type":"core.divider","props":{"style":"solid","weight":"2px","color":"#ccc"}},
		{"id":"d2","type":"core.divider","props":{"inset":{"kind":"text","text":"或者 & 更多","position":"left","spacing":"8px","fontSize":"14px"}}},
		{"id":"d3","type":"core.divider","props":{"inset":{"kind":"icon","iconName":"star","position":"right"},"advanced":{"customClasses":["x"],"customId":"div-id"}}}
	]`)},
	{name: "spacer", doc: rootDoc(`[
		{"id":"s1","type":"core.spacer","props":{"height":{"desktop":"80px","tablet":"40px","mobile":"20px"},"advanced":{"margin":{"desktop":{"top":"10px","bottom":"20px"}}}}}
	]`)},
	{name: "list", doc: rootDoc(`[
		{"id":"l1","type":"core.list","props":{"style":"icon","items":[{"icon":"check","text":"第一项 & 特殊","link":"https://example.com/1"},{"icon":"star","text":"第二项"},{"text":"无图标项"}]}},
		{"id":"l2","type":"core.list","props":{"style":"number","items":[{"text":"甲"},{"text":"乙"}]}},
		{"id":"l3","type":"core.list","props":{"style":"dot","items":[{"text":"点1"},{"text":"点2"}]}}
	]`)},
	{name: "infobox", doc: rootDoc(`[
		{"id":"ib1","type":"core.infobox","props":{"icon":"check","title":"标题 & 内容","text":"描述","subtitle":"副标题","link":"https://example.com","btnText":"了解更多"}},
		{"id":"ib2","type":"core.infobox","props":{"mediaImage":"https://example.com/m.jpg","title":"媒体图","text":"文本"}},
		{"id":"ib3","type":"core.infobox","props":{"icon":"star","title":"链接卡","link":"https://example.com/2"}}
	]`)},
	{name: "socialbuttons", doc: rootDoc(`[
		{"id":"sb1","type":"core.social_buttons","props":{"color":"brand","items":[{"platform":"facebook","url":"https://facebook.com/x"},{"platform":"x","url":"https://x.com/y"}]}},
		{"id":"sb2","type":"core.social_buttons","props":{"color":"custom","customColor":"#123456","items":[{"platform":"youtube","url":"https://youtube.com/z"}]}}
	]`)},
	{name: "video", doc: rootDoc(`[
		{"id":"v1","type":"core.video","props":{"url":"https://www.youtube.com/watch?v=abc12345","ratio":"16:9"}},
		{"id":"v2","type":"core.video","props":{"url":"https://example.com/storage/v.mp4","poster":"https://example.com/p.jpg","controls":true,"muted":true,"loop":true,"preload":"auto","ratio":"4:3","fullWidth":true}}
	]`)},
	{name: "counter", doc: rootDoc(`[
		{"id":"c1","type":"core.counter","props":{"start":0,"end":12345,"decimals":2,"prefix":"$","suffix":"+","label":"满意客户","duration":3,"color":"#111"}},
		{"id":"c2","type":"core.counter","props":{"end":99,"label":"百分比","suffix":"%"}}
	]`)},
	{name: "gallery", doc: rootDoc(`[
		{"id":"ga1","type":"core.gallery","props":{"mode":"grid","items":[{"url":"https://example.com/a.jpg","alt":"图一 & 说明","caption":"图注一"},{"url":"https://example.com/b.jpg","alt":"图二","link":"https://example.com/t1"}],"grid":{"columns":{"desktop":3,"tablet":2,"mobile":1},"columnGap":"16px","rowGap":"16px"},"aspectRatio":"16:9","radius":"8px","captionMode":"below"}},
		{"id":"ga2","type":"core.gallery","props":{"mode":"carousel","items":[{"url":"https://example.com/c.jpg","alt":"轮播一"},{"url":"https://example.com/d.jpg","alt":"轮播二"}],"carousel":{"autoplay":true,"interval":3000,"infinite":true,"pauseOnHover":true,"slidesPerView":{"desktop":2,"tablet":1,"mobile":1},"arrows":true,"dots":true},"captionMode":"hover","hover":{"scale":"1.05","overlay":"dark"}}}
	]`)},
	{name: "slider", doc: rootDoc(`[
		{"id":"sl1","type":"core.slider","props":{"perView":{"desktop":2,"tablet":1,"mobile":1},"autoplay":3.5,"showArrows":true,"showDots":true,"loop":true,"gap":"24px"},"children":[
			{"id":"sl1a","type":"core.text","props":{"mode":"plaintext","text":"第一屏"}},
			{"id":"sl1b","type":"core.text","props":{"mode":"plaintext","text":"第二屏 & 更多"}}
		]}
	]`)},
	{name: "tabs", doc: rootDoc(`[
		{"id":"tb1","type":"core.tabs","props":{"tabs":[{"label":"标签一"},{"label":"标签二 & 更多"},{"label":"标签三"}],"navAlign":"center","activeColor":"#2563eb"},"children":[
			{"id":"tb1a","type":"core.text","props":{"mode":"plaintext","text":"面板一"}},
			{"id":"tb1b","type":"core.text","props":{"mode":"plaintext","text":"面板二"}},
			{"id":"tb1c","type":"core.text","props":{"mode":"plaintext","text":"面板三"}}
		]},
		{"id":"tb2","type":"core.tabs","props":{"tabs":[{"label":"竖排"},{"label":"第二"}],"vertical":true},"children":[
			{"id":"tb2a","type":"core.text","props":{"mode":"plaintext","text":"竖一"}},
			{"id":"tb2b","type":"core.text","props":{"mode":"plaintext","text":"竖二"}}
		]}
	]`)},
	{name: "accordion", doc: rootDoc(`[
		{"id":"ac1","type":"core.accordion","props":{"items":[{"title":"第一个","open":true},{"title":"第二个 & 更多"}],"oneOpen":true,"bgColor":"#f9f9f9"},"children":[
			{"id":"ac1a","type":"core.text","props":{"mode":"plaintext","text":"内容一"}},
			{"id":"ac1b","type":"core.text","props":{"mode":"plaintext","text":"内容二"}}
		]},
		{"id":"ac2","type":"core.accordion","props":{"items":[{"title":"无边框"}],"borderless":true},"children":[
			{"id":"ac2a","type":"core.text","props":{"mode":"plaintext","text":"内容"}}
		]}
	]`)},
	{name: "marquee", doc: rootDoc(`[
		{"id":"mq1","type":"core.marquee","props":{"speed":15,"direction":"right","pauseOnHover":true,"gap":"32px","background":"#111","padding":"12px"},"children":[
			{"id":"mq1a","type":"core.text","props":{"mode":"plaintext","text":"滚动内容一"}},
			{"id":"mq1b","type":"core.text","props":{"mode":"plaintext","text":"滚动内容二 & 更多"}}
		]}
	]`)},
	{name: "globalref_placeholder", doc: rootDoc(`[
		{"id":"gr1","type":"core.globalref","props":{"blockId":"shared-header"}}
	]`)},
	{name: "globalref_expand", doc: rootDoc(`[
		{"id":"gr1","type":"core.globalref","props":{"blockId":"shared-header"}}
	]`), block: &mockBlockResolver{roots: map[string][]*core.Node{
		"shared-header": {
			{ID: "block-text", Type: "core.text", Props: json.RawMessage(`{"mode":"plaintext","text":"块内容 & 更多"}`)},
		},
	}}},
}

// goldenDir golden 文件目录。
const goldenDir = "testdata/golden"

// readGolden 读取 golden 文件内容（不存在时给出明确错误）。
// golden 由切换前旧 render 路径生成并固化，作为「切换前产物」基准。
func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goldenDir, name))
	if err != nil {
		t.Fatalf("读取 golden %s 失败: %v", name, err)
	}
	return string(b)
}

// TestJetViewByteEquivalent 断言新 Compile（Jet 路径）产物与 golden（切换前旧产物）字节一致。
func TestJetViewByteEquivalent(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}

	for _, c := range goldenCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			page, err := ParsePage([]byte(c.doc))
			if err != nil {
				t.Fatalf("ParsePage: %v", err)
			}
			opts := []CompileOption{WithComponentSet(set)}
			if c.block != nil {
				opts = append(opts, WithBlockResolver(c.block))
			}
			got, err := Compile(page, opts...)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if want := readGolden(t, c.name+".html"); got.HTML != want {
				t.Errorf("HTML 字节不一致（与切换前产物）:\n--- golden ---\n%s\n--- 新输出 ---\n%s", want, got.HTML)
			}
			if want := readGolden(t, c.name+".css"); got.CSS != want {
				t.Errorf("CSS 字节不一致（与切换前产物）:\n--- golden ---\n%s\n--- 新输出 ---\n%s", want, got.CSS)
			}
		})
	}
}
