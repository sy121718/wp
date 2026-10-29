package builder

// userforms_render_test.go — 访客账号表单组件的**产物级**验证。
//
// 组件包的测试覆盖 BuildView；模板对不对只能在编译产物里看：
// Jet 里一个写错的变量通常渲染成空串而不是报错。

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/templates"
	"golang.org/x/net/html"
)

// userFormsDoc 含一个账号表单节点的最小文档。
func userFormsDoc(t *testing.T, props map[string]any) *Page {
	t.Helper()
	doc, err := json.Marshal(map[string]any{
		"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
		"root":     []any{map[string]any{"id": "auth", "type": "core.userForms", "props": props}},
	})
	if err != nil {
		t.Fatalf("序列化文档失败: %v", err)
	}
	page, err := ParsePage(doc)
	if err != nil {
		t.Fatalf("文档解析失败: %v", err)
	}
	return page
}

// TestUserFormsCompilesStaticJSONForms 组件无工程也能发布，凭据不进入产物。
func TestUserFormsCompilesStaticJSONForms(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"login", "register", "forgot", "reset", "account", "profile", "preference", "password", "sessions"} {
		page := userFormsDoc(t, map[string]any{"mode": mode, "title": "加入我们", "showTitle": true, "next": "/account"})
		compiled, err := Compile(page, WithComponentSet(set))
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		doc, err := RenderDocument(compiled)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`data-user-form="` + mode + `"`, `data-component-instance="auth"`, `data-site-read="/api/site/user/session"`, `data-write-handler="user.forms"`, `data-write-url="/api/site/user/account/profile"`, `name="locale"`, `name="timezone"`, `data-user-next="/account"`, ">加入我们<"} {
			if !strings.Contains(doc, want) {
				t.Errorf("模式%s缺少%s", mode, want)
			}
		}
		for _, forbidden := range []string{`/_fragments/login`, `/_fragments/account`, `action="/user/`, `name="csrf_token"`, `hx-get=`} {
			if strings.Contains(doc, forbidden) {
				t.Errorf("模式%s残留旧链%s", mode, forbidden)
			}
		}
	}
}

func TestUserFormsCodeFlowPublishesUsableInputs(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(userFormsDoc(t, map[string]any{"mode": "register"}), WithComponentSet(set), WithCompileMode(CompileModePublish))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-write-url="/api/site/user/activate"`, `data-write-url="/api/site/user/resend"`, `data-user-register-entry`, `data-user-register-verify hidden`, `data-user-register-resend hidden`, `autocomplete="one-time-code"`, `minlength="8" maxlength="8"`, `发送重置验证码`, `邮箱验证码`} {
		if !strings.Contains(compiled.HTML, want) {
			t.Fatalf("默认账号流程缺少可操作内容%s", want)
		}
	}
	if strings.Contains(compiled.HTML, `type="hidden" name="key"`) {
		t.Fatal("邮箱验证码不能依赖隐藏字段或固定重置链接")
	}
	tree, err := html.Parse(strings.NewReader(compiled.HTML))
	if err != nil {
		t.Fatal(err)
	}
	var inspect func(*html.Node, bool)
	inspect = func(node *html.Node, inForm bool) {
		form := node.Type == html.ElementNode && node.Data == "form"
		if form && inForm {
			t.Fatal("账号流程不能嵌套form")
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			inspect(child, inForm || form)
		}
	}
	inspect(tree, false)
}
