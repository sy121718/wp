package feature

import (
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// assertI18nHTMLFragment 验证真实片段协议，不把 HTML 伪装成旧 JSON 信封。
func assertI18nHTMLFragment(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("词条响应必须是不可缓存的 HTML：%d %v %s", rec.Code, rec.Header(), body)
	}
	if strings.Contains(strings.ToLower(body), "<html") || rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" {
		t.Fatalf("局部响应不得导航或携带页面壳：%s", body)
	}
	if rec.Code >= 400 && (!strings.Contains(body, "data-ui-error") || strings.Contains(body, "<form")) {
		t.Fatalf("失败必须是错误片段，不能替换表单：%s", body)
	}
	assertPageNoLeak(t, "词条 HTML", body)
	return body
}

func i18nHTMLText(t *testing.T, body, tag string) []string {
	t.Helper()
	root, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	var text func(*html.Node) string
	text = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var out strings.Builder
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			out.WriteString(text(child))
		}
		return out.String()
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			texts = append(texts, strings.TrimSpace(text(n)))
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return texts
}

func i18nHTMLMessage(t *testing.T, body string) string {
	t.Helper()
	messages := i18nHTMLText(t, body, "p")
	if len(messages) != 1 || messages[0] == "" {
		t.Fatalf("应有唯一明确结果文案：%s", body)
	}
	return messages[0]
}
