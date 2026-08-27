package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/media"
)

// buttonDoc 构造 button 节点文档。
func buttonDoc(t *testing.T, props string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"b1","type":"core.button","props":`+props+`}]}`)
}

// TestButtonExternal 外部链接：单层 <a> + target/rel + 图标（内置 SVG）+ 双态 CSS。
func TestButtonExternal(t *testing.T) {
	props := `{"text":"立即抢购","action":"external","value":"https://example.com/checkout","target":"blank","rel":"nofollow","size":"lg","variant":"solid","radius":"9999","normal":{"background":"#2563eb","color":"#fff","shadow":"sm"},"hover":{"background":"#1d4ed8","color":"#fff","shadow":"md"},"hoverLift":"-2px","icon":{"source":"builtin","name":"arrow-right","position":"suffix","spacing":"8px","hoverShift":"4px"}}`
	c, err := builder.Compile(buttonDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	for _, want := range []string{
		`<a class="wp-c-b1" href="https://example.com/checkout" target="_blank" rel="noopener noreferrer nofollow">`,
		`<span class="bt-text">立即抢购</span>`,
		`<svg class="bt-icon bt-icon-shift"`, // 内置箭头图标
		"</a>",
	} {
		if !strings.Contains(c.HTML, want) {
			t.Errorf("HTML 缺少 %q\n%s", want, c.HTML)
		}
	}
	for _, want := range []string{
		"border-radius: 9999px",
		"background: #2563eb", "color: #fff", "box-shadow: 0 1px 3px rgba(0,0,0,0.12)",
		".wp-c-b1:hover, .wp-c-b1:focus", "transform: translateY(-2px)",
		"translateX(4px)",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, c.CSS)
		}
	}
}

// TestButtonModal 弹窗触发：原生 <button> + data-modal-target，无 href。
func TestButtonModal(t *testing.T) {
	props := `{"text":"联系商务合作","action":"modal","value":"contact-modal"}`
	c, err := builder.Compile(buttonDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		`<button class="wp-c-b1" type="button" data-modal-target="contact-modal">`,
		`<span class="bt-text">联系商务合作</span>`,
		"</button>",
	} {
		if !strings.Contains(c.HTML, want) {
			t.Errorf("HTML 缺少 %q\n%s", want, c.HTML)
		}
	}
	if strings.Contains(c.HTML, "href") {
		t.Errorf("modal 按钮不应含 href: %s", c.HTML)
	}
}

// TestButtonActions 其余动作：internal/anchor/native。
func TestButtonActions(t *testing.T) {
	cases := []struct{ name, props, wantHref string }{
		{"internal", `{"text":"进入产品页","action":"internal","value":"/products/keyboard-pro"}`, `href="/products/keyboard-pro"`},
		{"anchor", `{"text":"查看特性","action":"anchor","value":"features"}`, `href="#features"`},
		{"native", `{"text":"致电","action":"native","value":"tel:+8613800000000"}`, `href="tel:+8613800000000"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := builder.Compile(buttonDoc(t, tc.props))
			if err != nil {
				t.Fatalf("编译失败: %v", err)
			}
			if !strings.Contains(c.HTML, tc.wantHref) {
				t.Errorf("HTML 缺少 %q\n%s", tc.wantHref, c.HTML)
			}
		})
	}
}

// TestButtonLinkBinding 动态链接绑定：ContentResolver 解析。
func TestButtonLinkBinding(t *testing.T) {
	resolver := mapResolver{"product.permalink": "/products/keyboard-pro"}
	c, err := builder.Compile(buttonDoc(t, `{"text":"查看商品","action":"link","binding":{"field":"product.permalink"}}`),
		builder.WithContentResolver(resolver))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, `href="/products/keyboard-pro"`) {
		t.Errorf("动态链接未填充: %s", c.HTML)
	}
}

// TestButtonMediaIcon 媒体库 SVG 图标内联。
func TestButtonMediaIcon(t *testing.T) {
	s := media.NewStore()
	id, _, err := s.Upload(media.Asset{
		Hash: hash64("bic"), FileName: "check.svg", MimeType: "image/svg+xml",
		Type: media.TypeSVG, Width: 24, Height: 24,
		Variants: []media.Variant{{Kind: media.VariantOriginal, Format: "svg", URL: `<svg xmlns="http://www.w3.org/2000/svg"><path d="M1 1"/></svg>`, Width: 24, Height: 24}},
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	c, err := builder.Compile(buttonDoc(t, `{"text":"确认","action":"external","value":"https://a.com/ok","icon":{"source":"media","assetId":"`+id+`","position":"prefix"}}`),
		builder.WithMediaResolver(s))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, "<svg") || !strings.Contains(c.HTML, "bt-icon") {
		t.Errorf("媒体 SVG 图标未内联: %s", c.HTML)
	}
}

// TestButtonBlock 块级铺满三端。
func TestButtonBlock(t *testing.T) {
	c, err := builder.Compile(buttonDoc(t, `{"text":"加入购物车","action":"external","value":"https://a.com/cart","block":{"desktop":true,"mobile":true}}`))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Count(c.CSS, "width: 100%") < 2 {
		t.Errorf("三端块级铺满异常:\n%s", c.CSS)
	}
}

// TestButtonValidate 校验拒绝。
func TestButtonValidate(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"无文本无绑定", `{}`, "必须提供"},
		{"非法外链协议", `{"text":"x","action":"external","value":"javascript:alert(1)"}`, "外部链接非法"},
		{"内链非法路径", `{"text":"x","action":"internal","value":"http://evil.com"}`, "站内路径"},
		{"锚点非法ID", `{"text":"x","action":"anchor","value":"1bad"}`, "合法元素 ID"},
		{"原生仅限协议", `{"text":"x","action":"native","value":"ftp://x.com"}`, "仅支持 tel:/mailto:"},
		{"弹窗缺目标ID", `{"text":"x","action":"modal"}`, "弹窗绑定必须提供"},
		{"绑定仅限link动作", `{"text":"x","action":"external","value":"https://a.com","binding":{"field":"post.permalink"}}`, "仅支持 link 动作"},
		{"非法内置图标", `{"text":"x","action":"external","value":"https://a.com","icon":{"source":"builtin","name":"hacker"}}`, "无效的内置图标"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := builder.Compile(buttonDoc(t, tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}
}

// TestButtonDeterministic 确定性构建。
func TestButtonDeterministic(t *testing.T) {
	props := `{"text":"确定","action":"external","value":"https://a.com/ok","icon":{"source":"builtin","name":"check"}}`
	c1, err := builder.Compile(buttonDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	c2, err := builder.Compile(buttonDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if c1.HTML != c2.HTML || c1.CSS != c2.CSS {
		t.Error("同一文档两次编译输出不一致")
	}
}
