package main

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestCustomerBulkContractsUseUserDTOsThroughGenericBinder(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	bindings, err := scanHandlerBindings()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, handler, dto string }{
		{"/api/customer/bulk-status", "user:BulkCustomerStatus", "userdto.CustomerBulkStatusReq"},
		{"/api/customer/bulk-unlock", "user:BulkCustomerUnlock", "userdto.CustomerBulkUnlockReq"},
	} {
		if got := matchHandler(tc.path, moduleOfRoutePath(tc.path), bindings); got != tc.handler || bindings[got] != tc.dto {
			t.Fatalf("%s 未追踪到真实请求形状：handler=%q dto=%q", tc.path, got, bindings[got])
		}
	}
}

func TestDTOFieldsExpandEmbeddedRequestsAcrossFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"a.go": "package testdto\ntype ApplyReq struct { *RuleReq; Note string `form:\"note\" binding:\"required\"` }\ntype PreviewReq struct { RuleReq }\n",
		"z.go": "package testdto\ntype RuleReq struct { ProjectID string `form:\"projectId\" binding:\"required\"`; Items []Item `json:\"items\"`; Secret string `json:\"-\" form:\"-\"` }; type Item struct { ID string `form:\"id\"` }\n",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := scanDTOsFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	base := dtoInfo{Fields: []fieldSchema{{Name: "projectId", Required: true}, {Name: "items"}}, Tables: []tableSchema{{Name: "items", Shape: "slice-struct"}}}
	if !reflect.DeepEqual(got["testdto.PreviewReq"], base) {
		t.Fatalf("匿名字段丢失：%+v", got["testdto.PreviewReq"])
	}
	base.Fields = append(base.Fields, fieldSchema{Name: "note", Required: true})
	base.Fields[0].Required = false // 未声明必填的匿名指针允许整个组合缺省。
	if !reflect.DeepEqual(got["testdto.ApplyReq"], base) {
		t.Fatalf("指针嵌入或直属字段丢失：%+v", got["testdto.ApplyReq"])
	}
}

func TestDTORequiredDistinguishesConditionsElementsAndOptionalParents(t *testing.T) {
	root := t.TempDir()
	data := "package testdto\ntype Child struct { Name string `form:\"name\" binding:\"required\"` }; type Optional struct { Child `binding:\"omitempty\"` }; type Forced struct { *Child `binding:\"required\"` }; type Fields struct { Key string `form:\"key\" binding:\"required_with=Version\"`; Version int `form:\"version\" binding:\"required_with=Key,omitempty,min=1\"`; OptionalIDs []string `form:\"optionalIds\" binding:\"dive,required\"`; IDs []string `form:\"ids\" binding:\"required,dive,required\"` }\n"
	if err := os.WriteFile(filepath.Join(root, "dto.go"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := scanDTOsFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []fieldSchema{{Name: "key"}, {Name: "version"}, {Name: "optionalIds"}, {Name: "ids", Required: true}}
	if !reflect.DeepEqual(got["testdto.Fields"].Fields, want) || got["testdto.Optional"].Fields[0].Required || !got["testdto.Forced"].Fields[0].Required {
		t.Fatalf("条件、元素与父级可选性不应变成无条件必填：%+v", got)
	}
}

func TestDTOFieldsKeepNamedEmbeddedContainer(t *testing.T) {
	root := t.TempDir()
	data := "package testdto\ntype Container struct { Nested `json:\"nested\"` }; type Nested struct { ID string `form:\"id\"` }\n"
	if err := os.WriteFile(filepath.Join(root, "dto.go"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := scanDTOsFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	want := dtoInfo{Fields: []fieldSchema{{Name: "nested"}}, Tables: []tableSchema{{Name: "nested", Shape: "struct"}}}
	if !reflect.DeepEqual(got["testdto.Container"], want) {
		t.Fatalf("具名嵌入应保持嵌套：%+v", got["testdto.Container"])
	}
}

func TestDTOFieldsKeepJSONContainersHandledByFormBinder(t *testing.T) {
	root := t.TempDir()
	data := "package testdto\ntype Params struct { RuleParams string `json:\"ruleParams\" form:\"-\"`; OperatorID string `json:\"-\" form:\"-\"`; Amount string `json:\"-\" form:\"amount\"` }\n"
	if err := os.WriteFile(filepath.Join(root, "dto.go"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := scanDTOsFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	// form:"-" 只禁止反射绑定；有 json 名的容器可由 BindForm 接收。
	// 两个入口均标成 "-" 的服务端字段不得进入客户端契约。
	want := dtoInfo{Fields: []fieldSchema{{Name: "ruleParams"}, {Name: "amount"}}}
	if !reflect.DeepEqual(got["testdto.Params"], want) {
		t.Fatalf("双入口字段语义发生变化：%+v", got["testdto.Params"])
	}
}
