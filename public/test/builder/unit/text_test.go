package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// textDoc 构造 text 节点文档。
func textDoc(t *testing.T, props string) *builder.Page {
	t.Helper()
	return mustParse(t, `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"t1","type":"core.text","props":`+props+`}]}`)
}

// TestTextPlainMode 纯文本模式：单层 <p>/<span> 直出 + 转义。
func TestTextPlainMode(t *testing.T) {
	props := `{"mode":"plaintext","plainTag":"p","text":"这是一款兼顾便携与降噪的日常通勤耳机。","typography":{"desktop":{"fontSize":"0.875rem","textAlign":"center"}}}`
	c, err := compile(t, textDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	want := `<div class="wp-c-t1"><p>这是一款兼顾便携与降噪的日常通勤耳机。</p></div>`
	if c.HTML != want {
		t.Errorf("HTML 异常:\nwant %s\ngot  %s", want, c.HTML)
	}
	if !strings.Contains(c.CSS, "font-size: 0.875rem") {
		t.Errorf("字号未编译:\n%s", c.CSS)
	}
}

// TestTextPlainSpan 纯文本 span 标签 + XSS 转义。
func TestTextPlainSpan(t *testing.T) {
	props := `{"mode":"plaintext","plainTag":"span","text":"<script>alert(1)</script>"}`
	c, err := compile(t, textDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c.HTML, "<script>") {
		t.Errorf("纯文本未转义: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, "&lt;script&gt;") {
		t.Errorf("转义输出异常: %s", c.HTML)
	}
}

// TestTextRichMode 富文本模式：结构化片段 + 白名单清洗。
func TestTextRichMode(t *testing.T) {
	props := `{"mode":"richtext","text":"<p>核心使用指南：</p><ul><li>长按 3 秒开启蓝牙配对。</li></ul><script>alert(1)</script><img src=x onerror=alert(2)>"}`
	c, err := compile(t, textDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		"<p>核心使用指南：</p>",
		"<ul><li>长按 3 秒开启蓝牙配对。</li></ul>",
	} {
		if !strings.Contains(c.HTML, want) {
			t.Errorf("HTML 缺少 %q: %s", want, c.HTML)
		}
	}
	// img 属白名单标签：标签本身保留，但非法 src（src=x）与 onerror 事件属性被剥离。
	if !strings.Contains(c.HTML, "<img>") {
		t.Errorf("合法 img 标签应保留（属性剥离后）: %s", c.HTML)
	}
	for _, bad := range []string{"<script", "onerror=", "<input", "src=x"} {
		if strings.Contains(c.HTML, bad) {
			t.Errorf("危险内容未被清洗: %q in %s", bad, c.HTML)
		}
	}
}

// TestTextRichImg 富文本图片：合法 img 保留，javascript:/data: 协议 src 与事件属性剥离。
func TestTextRichImg(t *testing.T) {
	props := `{"mode":"richtext","text":"<p><img src=\"https://example.com/a.jpg\" alt=\"说明\" width=\"300\" loading=\"lazy\"></p><p><img src=\"javascript:alert(1)\" onerror=\"alert(2)\"></p>"}`
	c, err := compile(t, textDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	// 合法 img 完整保留。
	if !strings.Contains(c.HTML, `<img src="https://example.com/a.jpg" alt="说明" width="300" loading="lazy">`) {
		t.Errorf("合法 img 未保留: %s", c.HTML)
	}
	// 危险协议与事件属性剥离。
	for _, bad := range []string{"javascript:", "onerror="} {
		if strings.Contains(c.HTML, bad) {
			t.Errorf("危险内容未被清洗: %q in %s", bad, c.HTML)
		}
	}
}

// TestTextRichLink 富文本链接：白名单协议 + target/rel。
func TestTextRichLink(t *testing.T) {
	props := `{"mode":"richtext","text":"<p>查看<a href=\"javascript:alert(1)\">坏链接</a>与<a href=\"https://example.com\" target=\"_blank\" rel=\"nofollow\">好链接</a></p>"}`
	c, err := compile(t, textDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c.HTML, "javascript:") {
		t.Errorf("危险协议未被拦截: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, `<a href="https://example.com" target="_blank" rel="nofollow">`) {
		t.Errorf("安全链接属性被误删: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, ">坏链接</a>") {
		t.Errorf("非法 href 应保留文本: %s", c.HTML)
	}
}

// TestTextBinding 绑定 + fallback：富文本绑定正文。
func TestTextBinding(t *testing.T) {
	resolver := mapResolver{"post.content": "<p>绑定正文内容</p>"}
	props := `{"mode":"richtext","binding":{"field":"post.content","fallback":"<p>兜底</p>"}}`
	c, err := compile(t, textDoc(t, props), builder.WithContentResolver(resolver))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c.HTML, "<p>绑定正文内容</p>") {
		t.Errorf("绑定内容未填入: %s", c.HTML)
	}

	// 空值回退 Fallback。
	c2, err := compile(t, textDoc(t, props), builder.WithContentResolver(mapResolver{"post.content": ""}))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(c2.HTML, "<p>兜底</p>") {
		t.Errorf("Fallback 未生效: %s", c2.HTML)
	}
}

// TestTextExcerpt 摘要模式：富文本绑定长文 strip 标签截前 N 字符。
func TestTextExcerpt(t *testing.T) {
	resolver := mapResolver{"product.description": "<p>第一段产品说明文字</p><p>第二段内容</p>"}
	props := `{"mode":"richtext","binding":{"field":"product.description"},"excerpt":6}`
	c, err := compile(t, textDoc(t, props), builder.WithContentResolver(resolver))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c.HTML, "<p>") {
		t.Errorf("摘要模式不应包含标签: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, "第一段产品说") {
		t.Errorf("摘要内容异常: %s", c.HTML)
	}
}

// TestTextLineClampSpacing 截断 + 段落间距。
func TestTextLineClampSpacing(t *testing.T) {
	props := `{"mode":"richtext","text":"<p>长文</p>","lineClamp":3,"paragraphSpacing":"0.75rem","color":"#444"}`
	c, err := compile(t, textDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		"-webkit-line-clamp: 3",
		"margin-top: 0.75rem",
		"color: #444",
	} {
		if !strings.Contains(c.CSS, want) {
			t.Errorf("CSS 缺少 %q\n%s", want, c.CSS)
		}
	}
}

// TestTextValidateErrors 校验拒绝。
func TestTextValidateErrors(t *testing.T) {
	cases := []struct{ name, props, want string }{
		{"无内容", `{"mode":"plaintext"}`, "必须提供内容"},
		{"非法模式", `{"mode":"markdown","text":"x"}`, "不在选项内"},
		{"非法纯文本标签", `{"mode":"plaintext","plainTag":"div","text":"x"}`, "不在选项内"},
		{"纯文本过长", `{"mode":"plaintext","text":"` + strings.Repeat("很", 2001) + `"}`, "纯文本过长"},
		{"非法绑定路径", `{"mode":"plaintext","binding":{"field":"Post.x"}}`, "无效的绑定字段路径"},
		{"非法字号", `{"mode":"plaintext","text":"x","typography":{"desktop":{"fontSize":"2url"}}}`, "无效的"},
		{"非法对齐", `{"mode":"plaintext","text":"x","typography":{"desktop":{"textAlign":"middle"}}}`, "无效的"},
		{"非法颜色", `{"mode":"plaintext","text":"x","color":"red}body{y:1}"}`, "值非法"},
		{"截断越界", `{"mode":"plaintext","text":"x","lineClamp":12}`, "超出上限 10"},
		{"摘要越界", `{"mode":"richtext","text":"<p>x</p>","excerpt":500}`, "超出上限 400"},
		{"摘要限富文本", `{"mode":"plaintext","text":"x","excerpt":10}`, "仅限富文本"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, textDoc(t, tc.props))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("期望错误含 %q，实际: %v", tc.want, err)
			}
		})
	}

	// 带子节点：children 在节点层，叶子组件应拒绝。
	t.Run("带子节点", func(t *testing.T) {
		doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"t1","type":"core.text","props":{"mode":"plaintext","text":"x"},"children":[{"id":"c1","type":"core.text","props":{"mode":"plaintext","text":"y"}}]}]}`
		_, err := compile(t, mustParse(t, doc))
		if err == nil || !strings.Contains(err.Error(), "叶子节点") {
			t.Errorf("期望叶子节点错误，实际: %v", err)
		}
	})
}

// TestTextRichTagStripping 非白名单标签剥壳保留文本（如自定义 block 标签）。
func TestTextRichTagStripping(t *testing.T) {
	props := `{"mode":"richtext","text":"<div><p>保留文本</p><em>斜体</em></div><div>尾段</div>"}`
	c, err := compile(t, textDoc(t, props))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if strings.Contains(c.HTML, "<div>") {
		t.Errorf("div 应被剥壳: %s", c.HTML)
	}
	if !strings.Contains(c.HTML, "<p>保留文本</p>") {
		t.Errorf("剥壳后文本丢失: %s", c.HTML)
	}
}
