package presets

import (
	"go_wp/pkg/builder"
	_ "embed"
	"strings"
	"testing"

	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
	"golang.org/x/net/html"
)

//go:embed user-account.v1.json
var accountPresetTestDocument []byte

func TestUserAccountPresetCompilesEveryControlledFormAndAction(t *testing.T) {
	page, err := builder.ParsePage(accountPresetTestDocument)
	if err != nil {
		t.Fatal(err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := builder.Compile(page, builder.WithComponentSet(set), builder.WithCompileMode(builder.CompileModePublish))
	if err != nil {
		t.Fatal(err)
	}
	document, err := builder.RenderDocument(compiled)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"login", "register", "forgot", "reset", "account", "profile", "preference", "password", "sessions"} {
		if strings.Count(document, `data-component-part="`+mode+`"`) != 1 {
			t.Fatalf("预设缺失或重复真实区域%s", mode)
		}
	}
	if !strings.Contains(document, `data-component-action="authenticated" data-action-kind="local" data-action-instance="account-center" data-action-part="account"`) {
		t.Fatal("默认登录后动作未解析为当前实例账号面板")
	}
	if strings.Contains(document, `site-preview-mode`) || strings.Contains(document, `name="csrf_token"`) || strings.Contains(document, `/_fragments/`) {
		t.Fatal("静态发布不能烘入模拟模式、访客令牌或旧HTML出口")
	}
	tree, err := html.Parse(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	var visit func(*html.Node, bool)
	forms := 0
	visit = func(node *html.Node, inForm bool) {
		if node.Type == html.ElementNode && node.Data == "form" {
			if inForm {
				t.Fatal("账号预设出现嵌套表单")
			}
			inForm = true
			forms++
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child, inForm)
		}
	}
	visit(tree, false)
	if forms < 9 {
		t.Fatalf("完整账号流程表单缺失：%d", forms)
	}
	matched, err := builder.Compile(page, builder.WithComponentSet(set), builder.WithCompileMode(builder.CompileModePublish))
	if err != nil || compiled.HTML != matched.HTML || compiled.CSS != matched.CSS {
		t.Fatalf("同一账号预设编译不确定：%v", err)
	}
}

func TestUserAccountSensitiveActionRejectsExternalOrWrongPart(t *testing.T) {
	for _, target := range []core.ActionTarget{
		{Kind: core.ActionExternal, URL: "https://outside.example/account"},
		{Kind: core.ActionLocal, PartKey: "profile"},
	} {
		page, err := builder.ParsePage(accountPresetTestDocument)
		if err != nil {
			t.Fatal(err)
		}
		page.Root[0].Children[1].Actions["authenticated"] = target
		set, err := templates.NewEmbeddedComponentSet()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := builder.Compile(page, builder.WithComponentSet(set), builder.WithCompileMode(builder.CompileModePublish)); err == nil {
			t.Fatalf("敏感动作目标未拒绝：%+v", target)
		}
	}
}
