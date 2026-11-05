package builder

import (
	"testing"

	"go_wp/pkg/builder/core"
)

func TestEditorActionDeclarationsAreCopiesAndExcludeFixedCartAction(t *testing.T) {
	declarations := ComponentActions()
	if len(declarations["core.addToCart"]) != 0 {
		t.Fatal("当前商品加购不应暴露动作接线")
	}
	if len(declarations["core.userForms"]) != 1 || declarations["core.userForms"][0].Key != "authenticated" {
		t.Fatal("账号只提供登录后目标")
	}
	declarations["core.userForms"][0].AllowedKinds[0] = core.ActionExternal
	fresh := ComponentActions()
	if fresh["core.userForms"][0].AllowedKinds[0] == core.ActionExternal {
		t.Fatal("编辑副本污染组件声明")
	}
}
