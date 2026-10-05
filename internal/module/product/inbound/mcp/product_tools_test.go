package productmcp

// product_tools_test.go — 商品工具集的形状与指针语义断言。
//
// 写工具的行为（确认位 / 幂等）由 internal/mcp/write_test.go 覆盖；这里钉商品特有的三件事：
// projectId 必填（多站点隔离）、update 的指针语义（nil = 不改 / 空串 = 清空）、
// 以及 create **不收 SKU 编码**（那两条入口的规则是运营看着仓库列表做的决定）。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	productdto "go_wp/internal/module/product/dto"
)

// fakeProduct 商品读写服务的假实现。
type fakeProduct struct {
	created  []*productdto.CreateReq
	updated  []*productdto.UpdateReq
	deleted  []*productdto.DeleteReq
	getReq   *productdto.GetReq
	getCalls int
	listReq  *productdto.ListReq
}

func (f *fakeProduct) Get(_ context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
	f.getReq = req
	f.getCalls++
	return &productdto.ProductResp{
		ID: req.ID, ProjectID: req.ProjectID, Name: "维生素 C", Slug: "vitamin-c",
		Type: "variant", Status: "active", SKUCode: "VIT-C",
	}, nil
}

func (f *fakeProduct) List(_ context.Context, req *productdto.ListReq) ([]*productdto.ProductResp, error) {
	f.listReq = req
	return []*productdto.ProductResp{
		{ID: "p-9", Name: "维生素 C 泡腾片", SKUCode: "VIT-C", Type: "variant", Status: "active"},
	}, nil
}

func (f *fakeProduct) Create(_ context.Context, req *productdto.CreateReq) (*productdto.ProductResp, error) {
	f.created = append(f.created, req)
	return &productdto.ProductResp{ID: "p-1", ProjectID: req.ProjectID, Name: req.Name, Slug: req.Slug}, nil
}

func (f *fakeProduct) Update(_ context.Context, req *productdto.UpdateReq) (*productdto.ProductResp, error) {
	f.updated = append(f.updated, req)
	return &productdto.ProductResp{ID: req.ID, Name: "改后"}, nil
}

func (f *fakeProduct) Delete(_ context.Context, req *productdto.DeleteReq) error {
	f.deleted = append(f.deleted, req)
	return nil
}

func byName(t *testing.T, list []mcp.Tool, name string) mcp.Tool {
	t.Helper()
	for _, tl := range list {
		if tl.Name() == name {
			return tl
		}
	}
	t.Fatalf("工具集里没有 %s", name)
	return mcp.Tool{}
}

func call(t *testing.T, tool mcp.Tool, body string) mcp.Result {
	t.Helper()
	res, err := tool.Invoke(context.Background(), json.RawMessage(body))
	if err != nil {
		t.Fatalf("%s 调用失败：%v", tool.Name(), err)
	}
	return res
}

// TestProductToolsShape 四个工具都在，且 projectId 是每个工具的必填项。
func TestProductToolsShape(t *testing.T) {
	f := &fakeProduct{}
	list, err := Tools(f, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Fatalf("工具条数应为 5，实得 %d", len(list))
	}
	for _, name := range []string{"product_find", "product_get", "product_create", "product_update", "product_delete"} {
		schema := byName(t, list, name).Schema()
		if _, ok := schema.Properties["projectId"]; !ok {
			t.Errorf("%s 缺少 projectId", name)
		}
		if !strings.Contains(strings.Join(schema.Required, ","), "projectId") {
			t.Errorf("%s 的 projectId 必须是必填（多站点隔离）", name)
		}
	}
}

// TestProductGetPassesProjectScope 工程作用域必须真的传下去。
func TestProductGetPassesProjectScope(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	call(t, byName(t, list, "product_get"),
		`{"projectId":"11111111-1111-1111-1111-111111111111","id":"p-9"}`)
	if f.getReq == nil || f.getReq.ProjectID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("工程没传下去：%+v", f.getReq)
	}
	if f.getReq.ID != "p-9" {
		t.Fatalf("id 没传下去：%+v", f.getReq)
	}
}

// TestProductUpdateKeepsOmittedFieldsNil 没传的字段必须是 nil（= 不改），不能变成空串。
//
// 这是这一层最容易写错的地方：`*string` 的 nil 与 "" 在 service 里是两个意思
// （不改 / 清空），一旦在工具层把缺省折算成空串，用户说「改个名字」就会顺手把副标题清掉 ——
// 而那次调用会成功返回。
func TestProductUpdateKeepsOmittedFieldsNil(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	call(t, byName(t, list, "product_update"),
		`{"projectId":"p-1","id":"p-9","name":"新名字","confirm":true,"idempotencyKey":"k1"}`)
	if len(f.updated) != 1 {
		t.Fatalf("应写一次，实得 %d", len(f.updated))
	}
	got := f.updated[0]
	if got.Name == nil || *got.Name != "新名字" {
		t.Errorf("name 没传对：%v", got.Name)
	}
	if got.Slug != nil {
		t.Errorf("没传 slug 时应是 nil（不改），实得 %q", *got.Slug)
	}
	if got.Subtitle != nil {
		t.Errorf("没传 subtitle 时应是 nil（不改），实得 %q", *got.Subtitle)
	}
}

// TestProductUpdateEmptyStringMeansClear 传空串才是清空。
//
// 这条与上一条是一对：把「不改」与「清空」分开是这次接口能成立的前提，
// 只测一个方向的话，把两者合并成一种的实现照样能过。
func TestProductUpdateEmptyStringMeansClear(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	call(t, byName(t, list, "product_update"),
		`{"projectId":"p-1","id":"p-9","subtitle":"","confirm":true,"idempotencyKey":"k2"}`)
	got := f.updated[0]
	if got.Subtitle == nil {
		t.Fatal("显式传空串应是「清空」（非 nil），实得 nil")
	}
	if *got.Subtitle != "" {
		t.Fatalf("副标题应被清空，实得 %q", *got.Subtitle)
	}
}

// TestProductCreateHasNoSkuArg create 不接受 SKU 编码。
func TestProductCreateHasNoSkuArg(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	props := byName(t, list, "product_create").Schema().Properties
	for _, k := range []string{"sku", "skuCode", "warehouseId", "warehouseSku"} {
		if _, ok := props[k]; ok {
			t.Errorf("create 不该接受 %q —— SKU 与仓库那套规则是运营在做决定，不是模型猜得出来的", k)
		}
	}
	// 但 projectId 与 name 必须在。
	for _, k := range []string{"projectId", "name"} {
		if _, ok := props[k]; !ok {
			t.Errorf("create 缺少 %q", k)
		}
	}
	// 而且 SKUCode 要留空交给 service 派生。
	call(t, byName(t, list, "product_create"),
		`{"projectId":"p-1","name":"新品","confirm":true,"idempotencyKey":"k3"}`)
	if f.created[0].SKUCode != "" {
		t.Errorf("SKUCode 应留空由 service 派生，实得 %q", f.created[0].SKUCode)
	}
}

// TestProductDeleteCarriesScopeAndConfirm 删除要同时有工程与确认。
func TestProductDeleteCarriesScopeAndConfirm(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	tool := byName(t, list, "product_delete")
	// 没有确认位时不执行。
	if _, err := tool.Invoke(context.Background(), json.RawMessage(
		`{"projectId":"p-1","id":"p-9","idempotencyKey":"k4"}`)); err == nil {
		t.Fatal("缺 confirm 应被拒")
	}
	if len(f.deleted) != 0 {
		t.Fatal("被拒时不该执行删除")
	}
	call(t, tool, `{"projectId":"p-1","id":"p-9","confirm":true,"idempotencyKey":"k4"}`)
	if len(f.deleted) != 1 || f.deleted[0].ProjectID != "p-1" {
		t.Fatalf("删除没带工程：%+v", f.deleted)
	}
}

// TestProductToolsRejectMissingDeps 依赖缺失是装配期错误。
func TestProductToolsRejectMissingDeps(t *testing.T) {
	f := &fakeProduct{}
	if _, err := Tools(nil, f, nil); err == nil {
		t.Error("读依赖缺失应报错")
	}
	if _, err := Tools(f, nil, nil); err == nil {
		t.Error("写依赖缺失应报错")
	}
}

// TestProductFindTurnsNameIntoID 搜索工具是「用户说商品名」这条路的唯一入口。
//
// 实测过它的缺位：给模型一个 uuid 它会正确要求补 projectId，
// 但**没人会那样说话** —— 用户说的是「那个维生素 C」，模型得有办法把名字换成 id。
func TestProductFindTurnsNameIntoID(t *testing.T) {
	f := &fakeProduct{}
	list, err := Tools(f, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := call(t, byName(t, list, "product_find"), `{"projectId":"p-1","keyword":"维生素"}`)
	if f.listReq == nil || f.listReq.Keyword != "维生素" {
		t.Fatalf("关键词没传下去：%+v", f.listReq)
	}
	if f.listReq.ProjectID != "p-1" {
		t.Fatalf("搜索也要带工程作用域：%+v", f.listReq)
	}
	if !strings.Contains(res.Text, "维生素 C 泡腾片") || !strings.Contains(res.Text, "p-9") {
		t.Fatalf("结果要同时给出名称与 id，实得：%s", res.Text)
	}
}

// TestProductGetPointsAtFind get 的说明要把「先 find」写出来。
func TestProductGetPointsAtFind(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	if !strings.Contains(byName(t, list, "product_get").Description(), "product_find") {
		t.Fatal("product_get 的说明里要指出 id 从哪来")
	}
}
