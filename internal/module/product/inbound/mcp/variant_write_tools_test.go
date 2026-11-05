package productmcp

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/mcp"
	productdto "go_wp/internal/module/product/dto"
)

type stubVariantWriter struct {
	createReq *productdto.CreateVariantReq
	createRes *productdto.VariantResp
	createErr error

	updateReq *productdto.UpdateVariantReq
	updateRes *productdto.VariantResp
	updateErr error

	deleteReq *productdto.DeleteVariantReq
	deleteErr error
}

func (s *stubVariantWriter) CreateVariant(_ context.Context, req *productdto.CreateVariantReq) (*productdto.VariantResp, error) {
	s.createReq = req
	return s.createRes, s.createErr
}

func (s *stubVariantWriter) UpdateVariant(_ context.Context, req *productdto.UpdateVariantReq) (*productdto.VariantResp, error) {
	s.updateReq = req
	return s.updateRes, s.updateErr
}

func (s *stubVariantWriter) DeleteVariant(_ context.Context, req *productdto.DeleteVariantReq) error {
	s.deleteReq = req
	return s.deleteErr
}

func variantTool(t *testing.T, name string, w *stubVariantWriter) mcp.Tool {
	t.Helper()
	tools, err := VariantWriteTools(w)
	if err != nil {
		t.Fatalf("装配变体写工具失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有工具 %q", name)
	return mcp.Tool{}
}

func invokeVariant(t *testing.T, tool mcp.Tool, args map[string]any) (string, error) {
	t.Helper()
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	res, err := tool.Invoke(mcp.WithUserID(context.Background(), 9), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func TestVariantWriteRejectsNilWriter(t *testing.T) {
	if _, err := VariantWriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestVariantWriteToolNames(t *testing.T) {
	tools, err := VariantWriteTools(&stubVariantWriter{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("应有 3 个工具，实得 %d", len(tools))
	}
	for _, name := range []string{"variant_create", "variant_update", "variant_delete"} {
		found := false
		for _, tool := range tools {
			if tool.Name() == name {
				found = true
			}
		}
		if !found {
			t.Errorf("缺工具 %q", name)
		}
	}
}

func TestVariantCreateMoneyIsYuanFloat(t *testing.T) {
	stub := &stubVariantWriter{createRes: &productdto.VariantResp{ID: "v1", ProductID: "p1", SKUCode: "SKU-1", Price: 99.5, Enabled: true}}
	_, err := invokeVariant(t, variantTool(t, "variant_create", stub), map[string]any{
		"productId": "p1", "price": 99.5, "comparePrice": 199, "costPrice": 40,
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	req := stub.createReq
	if req == nil {
		t.Fatal("service 未收到请求")
	}

	if req.Price == nil || *req.Price != 99.5 {
		t.Errorf("售价应原样传 99.5（元），实得 %v", req.Price)
	}
	if req.ComparePrice == nil || *req.ComparePrice != 199 {
		t.Errorf("划线价传错: %v", req.ComparePrice)
	}
	if req.CostPrice == nil || *req.CostPrice != 40 {
		t.Errorf("成本价传错: %v", req.CostPrice)
	}
}

func TestVariantMoneySchemaIsNumber(t *testing.T) {
	tool := variantTool(t, "variant_create", &stubVariantWriter{})
	raw, err := tool.SchemaJSON()
	if err != nil {
		t.Fatalf("取 schema 失败: %v", err)
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("解析 schema 失败: %v", err)
	}
	for _, field := range []string{"price", "comparePrice", "costPrice"} {
		got := schema.Properties[field].Type
		if got != "number" {
			t.Errorf("字段 %s 的类型应为 number（金额是元、可带小数），实得 %q", field, got)
		}
	}
}

func TestVariantCreateQuantitySemantics(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"不给数量 = 无限", map[string]any{"productId": "p1"}, "没有跟踪数量"},
		{"给 0 = 没货", map[string]any{"productId": "p1", "quantity": 0}, "卖不出去"},
		{"给 5 = 有货", map[string]any{"productId": "p1", "quantity": 5}, "已设为 5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := &stubVariantWriter{createRes: &productdto.VariantResp{ID: "v1", Enabled: true}}
			text, err := invokeVariant(t, variantTool(t, "variant_create", stub), c.args)
			if err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if !strings.Contains(text, c.want) {
				t.Errorf("回执应包含 %q，实得：%s", c.want, text)
			}
		})
	}
}

func TestVariantCreateQuantityReachesServiceAsNilOrZero(t *testing.T) {

	stub := &stubVariantWriter{createRes: &productdto.VariantResp{ID: "v1"}}
	if _, err := invokeVariant(t, variantTool(t, "variant_create", stub), map[string]any{"productId": "p1"}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.createReq.Quantity != nil {
		t.Errorf("不给数量时应为 nil，实得 %v", *stub.createReq.Quantity)
	}

	stub2 := &stubVariantWriter{createRes: &productdto.VariantResp{ID: "v1"}}
	if _, err := invokeVariant(t, variantTool(t, "variant_create", stub2), map[string]any{"productId": "p1", "quantity": 0}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub2.createReq.Quantity == nil || *stub2.createReq.Quantity != 0 {
		t.Errorf("显式给 0 时应为 0，实得 %v", stub2.createReq.Quantity)
	}
}

func TestVariantCreateWarnsWhenNotSellable(t *testing.T) {
	stub := &stubVariantWriter{createRes: &productdto.VariantResp{ID: "v1", Enabled: false}}
	text, err := invokeVariant(t, variantTool(t, "variant_create", stub), map[string]any{
		"productId": "p1", "enabled": false,
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "不可售") {
		t.Errorf("新建即不可售时必须提示，实得：%s", text)
	}
}

func TestVariantUpdateRejectsEmptyPatch(t *testing.T) {
	stub := &stubVariantWriter{}
	_, err := invokeVariant(t, variantTool(t, "variant_update", stub), map[string]any{"id": "v1"})
	if err == nil {
		t.Fatal("没有任何要改的字段时应报错")
	}
	if stub.updateReq != nil {
		t.Error("参数不合法时不应该调用 service")
	}
}

func TestVariantUpdateOnlyTouchesGivenFields(t *testing.T) {
	stub := &stubVariantWriter{updateRes: &productdto.VariantResp{ID: "v1", SKUCode: "SKU-1", Price: 88, Enabled: true}}
	if _, err := invokeVariant(t, variantTool(t, "variant_update", stub), map[string]any{
		"id": "v1", "price": 88,
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	req := stub.updateReq
	if req.Price == nil || *req.Price != 88 {
		t.Fatalf("价格没传到: %v", req.Price)
	}
	if req.SKUCode != nil || req.Barcode != nil || req.Image != nil || req.Enabled != nil ||
		req.Sort != nil || req.ComparePrice != nil || req.CostPrice != nil {
		t.Errorf("没传的字段必须保持 nil（不改），实得 %+v", req)
	}
}

func TestVariantUpdateCanSetPriceZero(t *testing.T) {
	stub := &stubVariantWriter{updateRes: &productdto.VariantResp{ID: "v1", Enabled: true}}
	if _, err := invokeVariant(t, variantTool(t, "variant_update", stub), map[string]any{
		"id": "v1", "price": 0,
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.updateReq.Price == nil || *stub.updateReq.Price != 0 {
		t.Errorf("显式改 0 必须传 0，实得 %v", stub.updateReq.Price)
	}
}

func TestVariantUpdatedTextReportsCurrentValues(t *testing.T) {
	cp := 199.0
	stub := &stubVariantWriter{updateRes: &productdto.VariantResp{
		ID: "v1", SKUCode: "SKU-1", Price: 88, ComparePrice: &cp, Enabled: false,
	}}
	text, err := invokeVariant(t, variantTool(t, "variant_update", stub), map[string]any{"id": "v1", "enabled": false})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}

	for _, want := range []string{"88", "199", "不可售"} {
		if !strings.Contains(text, want) {
			t.Errorf("回执应含当前值 %q，实得：%s", want, text)
		}
	}
}

func TestVariantDeleteTextPointsToSofterOption(t *testing.T) {
	stub := &stubVariantWriter{}
	text, err := invokeVariant(t, variantTool(t, "variant_delete", stub), map[string]any{"id": "v1"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.deleteReq == nil || stub.deleteReq.ID != "v1" {
		t.Fatalf("变体 id 没传到: %+v", stub.deleteReq)
	}

	if !strings.Contains(text, "下架") {
		t.Errorf("删除回执应提示下架这个更软的动作，实得：%s", text)
	}
	if !strings.Contains(text, "可逆") {
		t.Errorf("删除回执应说明下架可逆，实得：%s", text)
	}
}

func TestVariantDeleteDescriptionWarnsAgainstOveruse(t *testing.T) {
	tool := variantTool(t, "variant_delete", &stubVariantWriter{})
	desc := tool.Description()
	if !strings.Contains(desc, "下架") {
		t.Errorf("删除工具的描述必须写清「只是不想卖就用下架」：\n%s", desc)
	}
}
