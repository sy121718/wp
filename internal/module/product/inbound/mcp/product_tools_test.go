package productmcp

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/mcp"
	productdto "go_wp/internal/module/product/dto"
)

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
		Status: "active", SKUCode: "VIT-C",
	}, nil
}

func (f *fakeProduct) List(_ context.Context, req *productdto.ListReq) ([]*productdto.ProductResp, error) {
	f.listReq = req
	return []*productdto.ProductResp{
		{ID: "p-9", Name: "维生素 C 泡腾片", SKUCode: "VIT-C", Status: "active"},
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

func TestProductCreateHasNoSkuArg(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	props := byName(t, list, "product_create").Schema().Properties
	for _, k := range []string{"sku", "skuCode", "warehouseId", "warehouseSku"} {
		if _, ok := props[k]; ok {
			t.Errorf("create 不该接受 %q —— SKU 与仓库那套规则是运营在做决定，不是模型猜得出来的", k)
		}
	}

	for _, k := range []string{"projectId", "name"} {
		if _, ok := props[k]; !ok {
			t.Errorf("create 缺少 %q", k)
		}
	}

	call(t, byName(t, list, "product_create"),
		`{"projectId":"p-1","name":"新品","confirm":true,"idempotencyKey":"k3"}`)
	if f.created[0].SKUCode != "" {
		t.Errorf("SKUCode 应留空由 service 派生，实得 %q", f.created[0].SKUCode)
	}
}

func TestProductDeleteCarriesScopeAndConfirm(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	tool := byName(t, list, "product_delete")

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

func TestProductToolsRejectMissingDeps(t *testing.T) {
	f := &fakeProduct{}
	if _, err := Tools(nil, f, nil); err == nil {
		t.Error("读依赖缺失应报错")
	}
	if _, err := Tools(f, nil, nil); err == nil {
		t.Error("写依赖缺失应报错")
	}
}

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

func TestProductGetPointsAtFind(t *testing.T) {
	f := &fakeProduct{}
	list, _ := Tools(f, f, nil)
	if !strings.Contains(byName(t, list, "product_get").Description(), "product_find") {
		t.Fatal("product_get 的说明里要指出 id 从哪来")
	}
}

func TestDescribeProductReportsStockUnknown(t *testing.T) {
	unknown := &productdto.ProductResp{
		ID: "p-unknown", ProjectID: "proj-1", Name: "故障商品", StockUnknown: true,
	}
	got := describeProduct(unknown)
	if !strings.Contains(got, "读取失败") {
		t.Fatalf("库存读取失败必须如实说明，实际文本：%q", got)
	}
	if strings.Contains(got, "未入库") {
		t.Fatalf("故障不是「未入库」：两种事实必须显示成不同的话，实际：%q", got)
	}
	if strings.Contains(got, "不限") || strings.Contains(got, "各仓合计") {
		t.Fatalf("故障时不得伪造三态结论，实际：%q", got)
	}

	none := &productdto.ProductResp{ID: "p-none", ProjectID: "proj-1", Name: "未入库商品"}
	if got := describeProduct(none); !strings.Contains(got, "未入库") {
		t.Fatalf("没有任何库存行仍应说「未入库」，实际：%q", got)
	}
}

func TestDescribeProductStockUnknownLanguageInvariant(t *testing.T) {
	unknown := &productdto.ProductResp{ID: "p-unknown", ProjectID: "proj-1", Name: "Broken", StockUnknown: true}
	first := describeProduct(unknown)
	second := describeProduct(unknown)
	if first != second {
		t.Fatalf("同一份事实两次描述必须逐字相同（MCP 文本不带状态）：%q vs %q", first, second)
	}
	if strings.Contains(first, "stockUnknown") {
		t.Fatalf("说明里不应出现裸字段名，实际：%q", first)
	}
}
