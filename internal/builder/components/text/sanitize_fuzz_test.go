package text

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// FuzzSanitizeRichHTML 富文本消毒不变量：
//  1. 属性上下文无 XSS 载体：解析输出 HTML，任何标签属性不得以 on 开头
//     （onerror/onload/onclick/onmouseover…），href/src 不得为
//     javascript:/vbscript:/data: 协议。纯文本中的 "onerror=" 无害，不算漏洞。
//  2. 幂等：sanitize(sanitize(x)) == sanitize(x) —— 双重转义会破坏该不变量
//     （历史上 1 < 2 曾被输出为 1 &amp;lt; 2，再跑一遍就变成 &amp;amp;lt;）。
//  3. 输出长度不膨胀失控。
func FuzzSanitizeRichHTML(f *testing.F) {
	seeds := []string{
		"",
		"纯文本",
		"1 < 2",
		"1 &lt; 2",
		"a &amp; b",
		"<script>alert(1)</script>",
		"<script>1 &lt; 2</script>",
		`<img src=x onerror=alert(1)>`,
		`<img src=x onerror = alert(1)>`,
		`<a href="javascript:alert(1)">点我</a>`,
		`<a href="JAVASCRIPT:alert(1)">大小写</a>`,
		`<a href="data:text/html;base64,PHNjcmlwdD4=">data</a>`,
		`<a href="https://example.com" target="_blank">链接</a>`,
		`<svg onload=alert(1)></svg>`,
		"<b>粗</b><i>斜</i><u>下划线</u>",
		"&amp;&lt;&gt;&quot;&#39;",
		"<div><p>嵌套</p></div>",
		"超长文本" + strings.Repeat("x", 4096),
		"\x00\x01非法控制字符",
		"中文引号“”『』「」",
		"<br/><hr/>",
		"<style>body{}</style><link rel=x>",
		"关于 onerror 的教程文本",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) {
		out := sanitizeRichHTML(src)
		z := html.NewTokenizer(strings.NewReader(out))
		for {
			tt := z.Next()
			if tt == html.ErrorToken {
				break
			}
			if tt == html.StartTagToken || tt == html.SelfClosingTagToken || tt == html.EndTagToken {
				for {
					k, v, more := z.TagAttr()
					name := strings.ToLower(string(k))
					if strings.HasPrefix(name, "on") {
						t.Fatalf("输出含事件属性 %s=%q (sanitize(%q))", name, v, src)
					}
					if name == "href" || name == "src" {
						lv := strings.ToLower(strings.TrimSpace(string(v)))
						if strings.HasPrefix(lv, "javascript:") ||
							strings.HasPrefix(lv, "vbscript:") ||
							strings.HasPrefix(lv, "data:") {
							t.Fatalf("输出含危险 %s=%q (sanitize(%q))", name, v, src)
						}
					}
					if !more {
						break
					}
				}
			}
		}
		// 幂等性：二次消毒结果必须与首次一致。
		if again := sanitizeRichHTML(out); again != out {
			t.Fatalf("非幂等: sanitize(%q) = %q, 再消毒得 %q", src, out, again)
		}
		// 长度不爆炸（防畸形输入导致输出膨胀）。
		if len(out) > len(src)*4+4096 {
			t.Fatalf("输出膨胀: in=%d out=%d", len(src), len(out))
		}
	})
}
