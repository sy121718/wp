package inventorymcp

// inventory_tools_test.go — 库存两个只读工具的元信息、入参边界与正文可读性。
//
// 正文断言不是「测文案」：模型的回答完全来自 Text（Data 只给渲染层），
// 正文里少了哪个数，用户就看不到哪个数。这里钉的是几件会直接决定回答对错的事：
//   · 「不跟踪数量」必须与「0 件」分开说 —— quantity = 0 有两义（跟踪且卖光 /
//     不跟踪无限），只读数量会把「无限」在回答里写成「没货」，让人去做一次
//     没必要的补货；
//   · 空结果要给替代解释（线索写窄了 / 这个 SKU 没入库），而不是只回一个空；
//   · limit 必须夹取（工具不是分页页面）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	inventorydto "go_wp/internal/module/inventory/dto"
)

type stubReader struct {
	gotStock      *inventorydto.ListStockReq
	gotWarehouses *inventorydto.ListWarehouseReq

	stockRes     []*inventorydto.StockResp
	warehouseRes []*inventorydto.WarehouseResp
	err          error
}

func (s *stubReader) ListStocks(_ context.Context, req *inventorydto.ListStockReq) ([]*inventorydto.StockResp, error) {
	s.gotStock = req
	return s.stockRes, s.err
}

func (s *stubReader) ListWarehouses(_ context.Context, req *inventorydto.ListWarehouseReq) ([]*inventorydto.WarehouseResp, error) {
	s.gotWarehouses = req
	return s.warehouseRes, s.err
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化入参失败: %v", err)
	}
	return b
}

func mustTools(t *testing.T, stub *stubReader) map[string]mcp.Tool {
	t.Helper()
	tools, err := Tools(stub)
	if err != nil {
		t.Fatalf("取库存工具集失败: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("库存工具应暴露 2 个，实得 %d", len(tools))
	}
	byName := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name()] = tool
	}
	return byName
}

func TestToolsRejectsNilReader(t *testing.T) {
	if _, err := Tools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错（装配期接线缺陷要在启动时炸掉）")
	}
}

func TestStockFindRequiresProject(t *testing.T) {
	stub := &stubReader{}
	tools := mustTools(t, stub)
	_, err := tools["stock_find"].Invoke(context.Background(), mustRaw(t, map[string]any{}))
	if err == nil {
		t.Fatal("projectId 必填，缺失应被拦下")
	}
	var argsErr *mcp.ArgsError
	if !errors.As(err, &argsErr) {
		t.Fatalf("应是 *mcp.ArgsError，实得 %T（%v）", err, err)
	}
	if stub.gotStock != nil {
		t.Fatal("参数不合规时不应打到 service")
	}
}

func TestStockFindPassesAllClues(t *testing.T) {
	stub := &stubReader{stockRes: []*inventorydto.StockResp{}}
	tools := mustTools(t, stub)
	_, err := tools["stock_find"].Invoke(context.Background(), mustRaw(t, map[string]any{
		"projectId":   "p1",
		"warehouseId": "w1",
		"productId":   "prod1",
		"skuCode":     "VIT-C",
		"externalSku": "EXT-1",
		"limit":       7,
	}))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	got := stub.gotStock
	if got.ProjectID != "p1" || got.WarehouseID != "w1" || got.ProductID != "prod1" ||
		got.SKUCode != "VIT-C" || got.ExternalSKU != "EXT-1" {
		t.Fatalf("线索应逐条透传，实得 %+v", got)
	}
	if got.Size != 7 || got.Page != 1 {
		t.Fatalf("limit 应透传、Page 恒为 1，实得 size=%d page=%d", got.Size, got.Page)
	}
}

func TestStockFindClampsLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, stockDefaultLimit},
		{-4, stockDefaultLimit},
		{3, 3},
		{999, stockMaxLimit},
	}
	for _, tc := range cases {
		stub := &stubReader{stockRes: []*inventorydto.StockResp{}}
		tools := mustTools(t, stub)
		if _, err := tools["stock_find"].Invoke(context.Background(), mustRaw(t, map[string]any{"projectId": "p1", "limit": tc.in})); err != nil {
			t.Fatalf("调用失败: %v", err)
		}
		if stub.gotStock.Size != tc.want {
			t.Fatalf("limit %d 应夹取成 %d，实得 %d", tc.in, tc.want, stub.gotStock.Size)
		}
	}
}

// 这条是本文件最重要的一条：quantity = 0 有两义，正文必须把「不跟踪」说出来。
func TestStockTextDistinguishesUntrackedFromSoldOut(t *testing.T) {
	text := stockListText([]*inventorydto.StockResp{
		{SKUCode: "VIT-C", WarehouseName: "主仓", TrackQuantity: false, Quantity: 0},
		{SKUCode: "VIT-D", WarehouseName: "主仓", TrackQuantity: true, Quantity: 0},
	}, stockFindArgs{ProjectID: "p1"})
	if !strings.Contains(text, "不跟踪数量") {
		t.Fatalf("不跟踪的 SKU 必须明说（否则 0 会被读成没货）：\n%s", text)
	}
	if !strings.Contains(text, "0 件") {
		t.Fatalf("跟踪但卖光的应当照实给数量：\n%s", text)
	}
}

func TestStockTextCarriesWarehouseAndExternalSKU(t *testing.T) {
	text := stockListText([]*inventorydto.StockResp{
		{SKUCode: "VIT-C", WarehouseName: "上海主仓", TrackQuantity: true, Quantity: 42, ExternalSKU: "EXT-9"},
	}, stockFindArgs{ProjectID: "p1", SKUCode: "VIT-C"})
	for _, want := range []string{"VIT-C", "上海主仓", "42 件", "EXT-9", "SKU VIT-C"} {
		if !strings.Contains(text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, text)
		}
	}
}

func TestStockTextEmptyExplainsHowToNarrow(t *testing.T) {
	text := stockListText(nil, stockFindArgs{ProjectID: "p1", SKUCode: "NOPE"})
	if !strings.Contains(text, "没有查到库存行") {
		t.Fatalf("空结果应直说：%s", text)
	}
	if !strings.Contains(text, "SKU NOPE") {
		t.Fatalf("空结果要回显筛选范围，用户才知道自己筛了什么：%s", text)
	}
	if !strings.Contains(text, "product_find") || !strings.Contains(text, "warehouse_list") {
		t.Fatalf("空结果要给出下一步线索：%s", text)
	}
}

func TestWarehouseListTextMarksDefault(t *testing.T) {
	stub := &stubReader{warehouseRes: []*inventorydto.WarehouseResp{
		{ID: "w1", Name: "主仓", Code: "MAIN", Type: "own", Status: "active", IsDefault: true},
		{ID: "w2", Name: "海外仓", Code: "OS", Type: "third_party", Status: "active"},
	}}
	tools := mustTools(t, stub)
	out, err := tools["warehouse_list"].Invoke(context.Background(), mustRaw(t, map[string]any{"projectId": "p1"}))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	for _, want := range []string{"id=w1", "主仓", "MAIN", "默认仓", "id=w2", "海外仓"} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, out.Text)
		}
	}
}

func TestWarehouseListTextEmpty(t *testing.T) {
	if got := warehouseListText(nil); !strings.Contains(got, "还没有建仓库") {
		t.Fatalf("空清单应直说：%s", got)
	}
}
