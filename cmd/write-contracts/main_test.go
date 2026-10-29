package main

import (
	"os"
	"testing"
)

func TestRoleMenuSaveContractUsesAdminOwner(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatal(err)
		}
	})
	bindings, err := scanHandlerBindings()
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/role/menu/save"
	if got := matchHandler(path, moduleOfRoutePath(path), bindings); got != "admin:RoleMenuSave" {
		t.Fatalf("角色菜单写入口归属错误: %q", got)
	}
	if dto := bindings["admin:RoleMenuSave"]; dto != "admindto.RoleMenuSaveReq" {
		t.Fatalf("未使用权威绑定DTO: %s", dto)
	}
	if got := matchHandler(path, "role", map[string]string{"product:RoleMenuSave": "productdto.CreateReq"}); got != "" {
		t.Fatalf("不得跨模块匹配同名处理器: %s", got)
	}
}
