package aihttp

import (
	"testing"

	mw "go_wp/pkg/middleware"
)

// 未注入索引必须按拒绝处理：AI 路由装配早于商品/订单的登记，
// 装配方只有在目录核对通过后才构造不可变查询并注入。
func TestPermIndexRejectsBeforeInjection(t *testing.T) {
	var index *mw.PermIndex
	if ok, err := casbinAuthorizer(index, 1, "product:create"); err != nil || ok {
		t.Fatalf("未注入索引必须拒绝：ok=%v err=%v", ok, err)
	}
	empty := mw.BuildPermIndex(nil)
	if ok, err := casbinAuthorizer(empty, 1, "product:create"); err != nil || ok {
		t.Fatalf("空索引必须拒绝：ok=%v err=%v", ok, err)
	}
}

// 冻结后登记的后注册操作必须在同一份不可变索引里可见。
func TestPermIndexSeesAllRegistrations(t *testing.T) {
	index := mw.BuildPermIndex([]mw.RouteSpec{
		{Method: "POST", Path: "/api/content/create", Perm: "content:create", Kind: mw.RouteProtected,
			AuthObject: "/api/content/create", AuthMethod: "POST"},
		// 页面复用别的权限：它不该改变「这个权限码能做什么」的判定。
		{Method: "GET", Path: "/admin/articles", Perm: "content:list", Kind: mw.RoutePage,
			AuthObject: "/api/content/list", AuthMethod: "GET"},
	})
	if index.Has("product:create") {
		t.Fatal("索引外的权限码不应命中")
	}
	if !index.Has("content:create") {
		t.Fatal("已登记受保护操作应命中")
	}
	if index.Has("content:list") {
		t.Fatal("页面复用对象不应进入受保护操作查询")
	}
}

// 索引不可变：返回的是副本，调用方改不动内部状态。
func TestPermIndexReturnsCopies(t *testing.T) {
	index := mw.BuildPermIndex([]mw.RouteSpec{
		{Method: "POST", Path: "/api/a", Perm: "a:write", Kind: mw.RouteProtected},
	})
	got := index.Of("a:write")
	if len(got) != 1 || got[0].Path != "/api/a" {
		t.Fatalf("索引应命中：%+v", got)
	}
	got[0].Path = "/api/mutated"
	if again := index.Of("a:write"); again[0].Path != "/api/a" {
		t.Fatalf("Of 必须返回副本：%+v", again)
	}
}

func TestPermIndexPermsSorted(t *testing.T) {
	index := mw.BuildPermIndex([]mw.RouteSpec{
		{Method: "POST", Path: "/api/b", Perm: "b:write", Kind: mw.RouteProtected},
		{Method: "POST", Path: "/api/a", Perm: "a:write", Kind: mw.RouteProtected},
	})
	perms := index.Perms()
	if len(perms) != 2 || perms[0] != "a:write" || perms[1] != "b:write" {
		t.Fatalf("权限码应排序返回：%v", perms)
	}
}
