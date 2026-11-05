package text

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

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

		if again := sanitizeRichHTML(out); again != out {
			t.Fatalf("非幂等: sanitize(%q) = %q, 再消毒得 %q", src, out, again)
		}

		if len(out) > len(src)*4+4096 {
			t.Fatalf("输出膨胀: in=%d out=%d", len(src), len(out))
		}
	})
}
