package builder

// userforms_render_test.go — 访客账号表单组件的**产物级**验证。
//
// 组件包的测试覆盖 BuildView；模板对不对只能在编译产物里看：
// Jet 里一个写错的变量通常渲染成空串而不是报错。

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/templates"
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

// TestUserFormsCompilesToFragmentMount 产物里必须是一个真能拉表单的容器。
func TestUserFormsCompilesToFragmentMount(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page := userFormsDoc(t, map[string]any{
		"mode": "register", "title": "加入我们", "showTitle": true, "next": "/account",
	})
	compiled, err := Compile(page, WithComponentSet(set), WithProjectID("proj-9"))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	for _, want := range []string{
		`hx-get="/_fragments/registerForm?`, // 形态映射到片段能力
		`hx-trigger="load"`,
		"projectId=proj-9",
		"next=%2Faccount",
		">加入我们<",                // 作者自定义标题
		`href="/user/register"`, // 无 JS 时的降级落点（内置页面）
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("产物缺少 %q；实际产物：%s", want, doc)
		}
	}
}

// TestUserFormsModesMapToFragments 五种形态在产物里各自指向自己的片段。
func TestUserFormsModesMapToFragments(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	for mode, fragment := range map[string]string{
		"login": "loginForm", "register": "registerForm",
		"forgot": "forgotForm", "reset": "resetForm", "account": "accountPanel",
	} {
		page := userFormsDoc(t, map[string]any{"mode": mode})
		compiled, cerr := Compile(page, WithComponentSet(set), WithProjectID("proj-1"))
		if cerr != nil {
			t.Fatalf("形态 %s 编译失败: %v", mode, cerr)
		}
		doc, rerr := RenderDocument(compiled)
		if rerr != nil {
			t.Fatalf("形态 %s 组装失败: %v", mode, rerr)
		}
		if !strings.Contains(doc, "/_fragments/"+fragment+"?") {
			t.Errorf("形态 %s 应指向片段 %s；实际产物：%s", mode, fragment, doc)
		}
	}
}

// TestUserFormsWithoutProjectStaysCompilable 缺工程 id 时降级为提示而不是构建失败。
func TestUserFormsWithoutProjectStaysCompilable(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page := userFormsDoc(t, map[string]any{"mode": "login"})
	compiled, err := Compile(page, WithComponentSet(set))
	if err != nil {
		t.Fatalf("缺工程 id 不该让整页编译失败: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("组装文档失败: %v", err)
	}
	if !strings.Contains(doc, "暂不可用") {
		t.Errorf("缺工程 id 时应渲染可见提示；实际产物：%s", doc)
	}
}
