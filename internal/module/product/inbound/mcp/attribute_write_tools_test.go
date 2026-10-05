package productmcp

// attribute_write_tools_test.go — 属性组（规格）工具的边界。
//
// 这一组的关键是 **IsVariation**：它决定这个属性组是不是变体的来源。
// 用户说「加个颜色属性」时想的是筛选条件，而它一旦是 true，用到它的商品
// 就会按这些取值组合出一批 SKU —— 建完再改回来比一开始就设对麻烦得多，
// 所以描述与回执都要把这件事说出来。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	productdto "go_wp/internal/module/product/dto"
)

type stubAttributeWriter struct {
	createReq *productdto.CreateAttributeReq
	createRes *productdto.AttributeResp
	updateReq *productdto.UpdateAttributeReq
	updateRes *productdto.AttributeResp
	deleteReq *productdto.DeleteAttributeReq
	list      []*productdto.AttributeResp
}

func (s *stubAttributeWriter) CreateAttribute(_ context.Context, req *productdto.CreateAttributeReq) (*productdto.AttributeResp, error) {
	s.createReq = req
	return s.createRes, nil
}

func (s *stubAttributeWriter) UpdateAttribute(_ context.Context, req *productdto.UpdateAttributeReq) (*productdto.AttributeResp, error) {
	s.updateReq = req
	return s.updateRes, nil
}

func (s *stubAttributeWriter) DeleteAttribute(_ context.Context, req *productdto.DeleteAttributeReq) error {
	s.deleteReq = req
	return nil
}

func (s *stubAttributeWriter) ListAttributes(_ context.Context, _ *productdto.ListAttributeReq) ([]*productdto.AttributeResp, error) {
	return s.list, nil
}

func attrTool(t *testing.T, name string, w *stubAttributeWriter) mcp.Tool {
	t.Helper()
	tools, err := AttributeTools(w)
	if err != nil {
		t.Fatalf("装配属性工具失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有工具 %q", name)
	return mcp.Tool{}
}

func invokeAttr(t *testing.T, tool mcp.Tool, args map[string]any) (string, error) {
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

func TestAttributeRejectsNilWriter(t *testing.T) {
	if _, err := AttributeTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestAttributeToolNames(t *testing.T) {
	tools, err := AttributeTools(&stubAttributeWriter{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("应有 4 个工具（1 读 + 3 写），实得 %d", len(tools))
	}
}

// 建组时给的取值要按顺序落成 Sort，空串要丢掉。
func TestAttributeCreateCarriesValues(t *testing.T) {
	stub := &stubAttributeWriter{createRes: &productdto.AttributeResp{
		ID: "a1", Name: "颜色",
		Values: []productdto.AttributeValueResp{{Label: "红"}, {Label: "蓝"}},
	}}
	_, err := invokeAttr(t, attrTool(t, "attribute_create", stub), map[string]any{
		"name": "颜色", "values": []string{"红", "", "蓝"},
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	req := stub.createReq
	if len(req.Values) != 2 {
		t.Fatalf("空取值应被丢掉，实得 %#v", req.Values)
	}
	if req.Values[0].Label != "红" || req.Values[1].Label != "蓝" {
		t.Errorf("取值顺序不对: %#v", req.Values)
	}
	if req.Values[0].Sort != 0 || req.Values[1].Sort != 1 {
		t.Errorf("Sort 应按顺序落，实得 %d / %d", req.Values[0].Sort, req.Values[1].Sort)
	}
}

// IsVariation 的两个方向都要在回执里说清后果。
func TestAttributeCreateTextExplainsVariation(t *testing.T) {
	yes := &stubAttributeWriter{createRes: &productdto.AttributeResp{
		ID: "a1", Name: "颜色", IsVariation: true,
		Values: []productdto.AttributeValueResp{{Label: "红"}},
	}}
	text, err := invokeAttr(t, attrTool(t, "attribute_create", yes), map[string]any{
		"name": "颜色", "isVariation": true, "values": []string{"红"},
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "生成变体") {
		t.Errorf("用于变体时必须说明会组合出 SKU，实得：%s", text)
	}

	no := &stubAttributeWriter{createRes: &productdto.AttributeResp{
		ID: "a2", Name: "材质", Values: []productdto.AttributeValueResp{{Label: "塑料"}},
	}}
	text2, err := invokeAttr(t, attrTool(t, "attribute_create", no), map[string]any{
		"name": "材质", "values": []string{"塑料"},
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text2, "不影响商品的变体结构") {
		t.Errorf("非变体属性应说明不影响变体，实得：%s", text2)
	}
}

// 建空属性组要给提醒：空属性组在商品编辑页里没法选。
func TestAttributeCreateWarnsWhenNoValues(t *testing.T) {
	stub := &stubAttributeWriter{createRes: &productdto.AttributeResp{ID: "a1", Name: "颜色"}}
	text, err := invokeAttr(t, attrTool(t, "attribute_create", stub), map[string]any{"name": "颜色"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "空属性组") {
		t.Errorf("没有取值时应提示，实得：%s", text)
	}
}

// 改不了取值这件事必须写进描述 —— 否则模型会试着自己拼一个。
func TestAttributeUpdateDescriptionSaysValuesNotEditable(t *testing.T) {
	desc := attrTool(t, "attribute_update", &stubAttributeWriter{}).Description()
	if !strings.Contains(desc, "改不了取值") {
		t.Errorf("描述必须说明取值不归这个工具管：\n%s", desc)
	}
}

func TestAttributeUpdateRejectsEmptyPatch(t *testing.T) {
	stub := &stubAttributeWriter{}
	if _, err := invokeAttr(t, attrTool(t, "attribute_update", stub), map[string]any{"id": "a1"}); err == nil {
		t.Fatal("没有任何要改的字段时应报错")
	}
	if stub.updateReq != nil {
		t.Error("参数不合法时不应该调用 service")
	}
}

func TestAttributeUpdateOnlyTouchesGivenFields(t *testing.T) {
	stub := &stubAttributeWriter{updateRes: &productdto.AttributeResp{ID: "a1", Name: "颜色"}}
	if _, err := invokeAttr(t, attrTool(t, "attribute_update", stub), map[string]any{
		"id": "a1", "name": "颜色",
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	req := stub.updateReq
	if req.Name == nil || *req.Name != "颜色" {
		t.Fatalf("名字没传到: %v", req.Name)
	}
	if req.Key != nil || req.IsVariation != nil || req.Sort != nil {
		t.Errorf("没传的字段必须保持 nil，实得 %+v", req)
	}
}

// 清单必须带 id 与「是否用于变体」，写工具的 id 只能从这里拿。
func TestAttributeListCarriesIDsAndVariationMark(t *testing.T) {
	stub := &stubAttributeWriter{list: []*productdto.AttributeResp{
		{ID: "a1", Name: "颜色", Key: "color", IsVariation: true,
			Values: []productdto.AttributeValueResp{{Label: "红"}, {Label: "蓝"}}},
		{ID: "a2", Name: "材质", Key: "material", IsVariation: false,
			Values: []productdto.AttributeValueResp{{Label: "塑料"}}},
	}}
	raw, _ := json.Marshal(map[string]any{})
	res, err := attrTool(t, "attribute_list", stub).Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	for _, want := range []string{"id=a1", "用于变体", "红", "蓝", "id=a2", "展示筛选"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("清单应含 %q，实得：%s", want, res.Text)
		}
	}
}

func TestAttributeListGuidesWhenEmpty(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{})
	res, err := attrTool(t, "attribute_list", &stubAttributeWriter{}).Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(res.Text, "attribute_create") {
		t.Errorf("空结果应指向 attribute_create，实得：%s", res.Text)
	}
}

func TestAttributeDeleteTextWarnsForVariation(t *testing.T) {
	stub := &stubAttributeWriter{}
	text, err := invokeAttr(t, attrTool(t, "attribute_delete", stub), map[string]any{"id": "a1"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.deleteReq == nil || stub.deleteReq.ID != "a1" {
		t.Fatalf("属性组 id 没传到: %+v", stub.deleteReq)
	}
	if !strings.Contains(text, "用于变体") {
		t.Errorf("删除回执应提醒用于变体的属性组会破坏规格结构，实得：%s", text)
	}
}
