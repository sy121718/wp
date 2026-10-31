package inventorymcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

type stubStore struct {
	saved map[string]mcp.Result
}

func newStubStore() *stubStore { return &stubStore{saved: map[string]mcp.Result{}} }

func (s *stubStore) Lookup(_ context.Context, tool, key string) (mcp.Result, bool, error) {
	res, ok := s.saved[tool+"|"+key]
	return res, ok, nil
}

func (s *stubStore) Save(_ context.Context, tool, key string, res mcp.Result) error {
	s.saved[tool+"|"+key] = res
	return nil
}

type stubStock struct {
	gotChange  *inventorydto.ChangeStockReq
	changeRes  *inventorydto.StockChangeResp
	changeErr  error
	reasons    []*inventorydto.ReasonResp
	reasonsErr error
}

func (s *stubStock) ChangeStock(_ context.Context, req *inventorydto.ChangeStockReq) (*inventorydto.StockChangeResp, error) {
	s.gotChange = req
	return s.changeRes, s.changeErr
}

func (s *stubStock) ListReasons(_ context.Context, _ *inventorydto.ListReasonReq) ([]*inventorydto.ReasonResp, error) {
	return s.reasons, s.reasonsErr
}

func (s *stubStock) ListStocks(_ context.Context, _ *inventorydto.ListStockReq) ([]*inventorydto.StockResp, error) {
	return nil, nil
}

func (s *stubStock) ListWarehouses(_ context.Context, _ *inventorydto.ListWarehouseReq) ([]*inventorydto.WarehouseResp, error) {
	return nil, nil
}

func mustWriteTools(t *testing.T, stub *stubStock) map[string]mcp.Tool {
	t.Helper()
	tools, err := WriteTools(stub, stub, newStubStore())
	if err != nil {
		t.Fatalf("装配写工具失败: %v", err)
	}
	out := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = tool
	}
	return out
}

func callStockChange(t *testing.T, stub *stubStock, args map[string]any) (string, error) {
	t.Helper()
	tool := mustWriteTools(t, stub)["stock_change"]
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化入参失败: %v", err)
	}
	res, err := tool.Invoke(context.Background(), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func baseChangeArgs() map[string]any {
	return map[string]any{
		"projectId":   "p-1",
		"warehouseId": "w-1",
		"direction":   inventoryenums.DirectionIn,
		"reasonCode":  "purchase_in",
		"lines":       []map[string]any{{"variantId": "v-1", "quantity": 5}},
	}
}

func TestWriteToolsRejectsNilDeps(t *testing.T) {
	if _, err := WriteTools(nil, &stubStock{}, newStubStore()); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
	if _, err := WriteTools(&stubStock{}, nil, newStubStore()); err == nil {
		t.Fatal("reader 为 nil 应报错（错误教学要用到它）")
	}
}

func TestStockChangeMapsArgs(t *testing.T) {
	stub := &stubStock{changeRes: &inventorydto.StockChangeResp{BatchID: "b-1"}}
	args := baseChangeArgs()
	args["remark"] = "采购单 PO-88"
	if _, err := callStockChange(t, stub, args); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.gotChange == nil {
		t.Fatal("没有透传到 service")
	}
	if stub.gotChange.Direction != inventoryenums.DirectionIn || stub.gotChange.ReasonCode != "purchase_in" {
		t.Errorf("方向 / 原因没传对: %+v", stub.gotChange)
	}
	if stub.gotChange.Remark != "采购单 PO-88" {
		t.Errorf("remark = %q", stub.gotChange.Remark)
	}
	if len(stub.gotChange.Lines) != 1 || stub.gotChange.Lines[0].Quantity != 5 {
		t.Errorf("行没传对: %+v", stub.gotChange.Lines)
	}

	if stub.gotChange.Lines[0].WarehouseID != "w-1" {
		t.Errorf("行内 warehouseId = %q，应当跟随顶层 warehouseId", stub.gotChange.Lines[0].WarehouseID)
	}
}

func TestStockChangeQuantitySignRules(t *testing.T) {
	cases := []struct {
		name      string
		direction string
		quantity  int
		wantErr   bool
	}{
		{"入库正数", inventoryenums.DirectionIn, 5, false},
		{"入库零", inventoryenums.DirectionIn, 0, true},
		{"入库负数", inventoryenums.DirectionIn, -5, true},
		{"出库正数", inventoryenums.DirectionOut, 3, false},
		{"出库负数", inventoryenums.DirectionOut, -3, true},
		{"盘点零（清零）", inventoryenums.DirectionAdjust, 0, false},
		{"盘点正数", inventoryenums.DirectionAdjust, 12, false},
		{"盘点负数", inventoryenums.DirectionAdjust, -1, true},
	}
	for _, c := range cases {
		stub := &stubStock{changeRes: &inventorydto.StockChangeResp{BatchID: "b"}}
		args := baseChangeArgs()
		args["direction"] = c.direction
		args["lines"] = []map[string]any{{"variantId": "v-1", "quantity": c.quantity}}
		_, err := callStockChange(t, stub, args)
		if c.wantErr && err == nil {
			t.Errorf("%s：应当报错", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s：不该报错，实得 %v", c.name, err)
		}
		if c.wantErr && err != nil && stub.gotChange != nil {
			t.Errorf("%s：被拒的请求不该到达 service", c.name)
		}
	}
}

func TestStockChangeRejectsEmptyAndTooManyLines(t *testing.T) {
	stub := &stubStock{}
	args := baseChangeArgs()
	args["lines"] = []map[string]any{}
	if _, err := callStockChange(t, stub, args); err == nil {
		t.Error("空 lines 应报错")
	}
	many := make([]map[string]any, 0, stockChangeMaxLines+1)
	for i := 0; i <= stockChangeMaxLines; i++ {
		many = append(many, map[string]any{"variantId": "v", "quantity": 1})
	}
	args["lines"] = many
	if _, err := callStockChange(t, stub, args); err == nil {
		t.Errorf("超过 %d 行应报错", stockChangeMaxLines)
	}
}

func TestStockChangeRejectsLineWithoutVariant(t *testing.T) {
	stub := &stubStock{}
	args := baseChangeArgs()
	args["lines"] = []map[string]any{{"quantity": 5}}
	if _, err := callStockChange(t, stub, args); err == nil {
		t.Error("缺 variantId 的行应报错")
	}
}

func TestStockChangeTeachesAvailableReasons(t *testing.T) {
	stub := &stubStock{
		changeErr: errors.New("原因与方向不符"),
		reasons: []*inventorydto.ReasonResp{
			{Code: "purchase_in", Name: "采购入库", Direction: inventoryenums.DirectionIn, IsBuiltin: true},
			{Code: "transfer_in", Name: "调拨入库", Direction: inventoryenums.DirectionIn},
		},
	}
	_, err := callStockChange(t, stub, baseChangeArgs())
	if err == nil {
		t.Fatal("service 报错时工具必须把错误传出去")
	}
	for _, want := range []string{"purchase_in", "transfer_in"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误里应带上可用 code %q：%v", want, err)
		}
	}
}

func TestStockChangeDoesNotTeachOnUnrelatedError(t *testing.T) {
	stub := &stubStock{
		changeErr: errors.New("库存不足，当前 2"),
		reasons:   []*inventorydto.ReasonResp{{Code: "purchase_in"}},
	}
	_, err := callStockChange(t, stub, baseChangeArgs())
	if err == nil {
		t.Fatal("应把错误传出去")
	}
	if strings.Contains(err.Error(), "可用的 reasonCode") {
		t.Errorf("库存不足的错误不该附原因清单：%v", err)
	}
	if !strings.Contains(err.Error(), "库存不足") {
		t.Errorf("原始错误文案要保留：%v", err)
	}
}

func TestChangeResultTextShowsBeforeAndAfter(t *testing.T) {
	stub := &stubStock{changeRes: &inventorydto.StockChangeResp{
		BatchID:     "b-77",
		CacheSynced: true,
		Movements: []*inventorydto.MovementResp{
			{SKUCode: "TEO-50-01", QuantityBefore: 3, QuantityAfter: 8, Delta: 5, WarehouseName: "苏州仓"},
		},
	}}
	text, err := callStockChange(t, stub, baseChangeArgs())
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"3 → 8", "TEO-50-01", "苏州仓", "b-77"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
}

func TestChangeResultTextWarnsOnCacheMiss(t *testing.T) {
	stub := &stubStock{changeRes: &inventorydto.StockChangeResp{
		BatchID:       "b-1",
		CacheSynced:   false,
		CacheFailures: []string{"product_totals"},
		Movements:     []*inventorydto.MovementResp{{SKUCode: "A", QuantityBefore: 1, QuantityAfter: 2, Delta: 1}},
	}}
	text, err := callStockChange(t, stub, baseChangeArgs())
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !strings.Contains(text, "缓存") {
		t.Errorf("缓存未同步应写出来：\n%s", text)
	}
}

func TestReasonsTextMarksBuiltinAndDirection(t *testing.T) {
	stub := &stubStock{reasons: []*inventorydto.ReasonResp{
		{Code: "purchase_in", Name: "采购入库", IsBuiltin: true},
		{Code: "my_custom", Name: "自定义原因"},
	}}
	tools := mustWriteTools(t, stub)
	raw, _ := json.Marshal(map[string]any{"projectId": "p-1"})
	res, err := tools["stock_reasons"].Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"purchase_in", "采购入库", "内置", "自建"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("正文缺 %q：\n%s", want, res.Text)
		}
	}
}

func TestReasonsTextSaysWhatToDoWhenEmpty(t *testing.T) {
	stub := &stubStock{}
	tools := mustWriteTools(t, stub)
	raw, _ := json.Marshal(map[string]any{"projectId": "p-1", "direction": inventoryenums.DirectionIn})
	res, err := tools["stock_reasons"].Invoke(context.Background(), raw)
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}

	if !strings.Contains(res.Text, "建") {
		t.Errorf("空结果应告诉用户下一步：\n%s", res.Text)
	}
}

func TestStockChangeIsIdempotent(t *testing.T) {
	stub := &stubStock{changeRes: &inventorydto.StockChangeResp{
		BatchID:   "b-1",
		Movements: []*inventorydto.MovementResp{{SKUCode: "A", QuantityBefore: 0, QuantityAfter: 5, Delta: 5}},
	}}
	store := newStubStore()
	tools, err := WriteTools(stub, stub, store)
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	tool := tools[1]
	raw, _ := json.Marshal(map[string]any{
		"projectId": "p-1", "warehouseId": "w-1", "direction": inventoryenums.DirectionIn,
		"reasonCode": "purchase_in", "lines": []map[string]any{{"variantId": "v", "quantity": 5}},
		"confirm": true, "idempotencyKey": "same-key",
	})
	if _, err := tool.Invoke(context.Background(), raw); err != nil {
		t.Fatalf("第一次调用失败: %v", err)
	}
	first := stub.gotChange
	if _, err := tool.Invoke(context.Background(), raw); err != nil {
		t.Fatalf("第二次调用失败: %v", err)
	}
	if stub.gotChange != first {
		t.Error("同一个幂等键的第二次调用不该再打到 service（那会重复扣/加库存）")
	}
}

func TestReasonNameNeverLeaksI18nKey(t *testing.T) {
	builtin := &inventorydto.ReasonResp{Code: "purchase_in", Name: "inventory.reason.purchase_in", IsBuiltin: true}
	got := reasonName(builtin)
	if strings.Contains(got, "inventory.reason.") {
		t.Errorf("内置原因名不该是裸 i18n key：%q", got)
	}
	if got != "purchase_in" && got != "采购入库" {
		t.Errorf("内置原因名应当是译文或 code，实得 %q", got)
	}

	custom := &inventorydto.ReasonResp{Code: "my_reason", Name: "供应商送样"}
	if reasonName(custom) != "供应商送样" {
		t.Errorf("自建原因名应当原样保留，实得 %q", reasonName(custom))
	}
}

func TestReasonsTextCarriesID(t *testing.T) {
	list := []*inventorydto.ReasonResp{
		{ID: "10", Code: "store_pickup_out", Name: "门店自提出库", Direction: "out"},
		{ID: "3", Code: "sale_out", Name: "inventory.reason.sale_out", Direction: "out", IsBuiltin: true},
	}
	text := reasonsText("out", list)
	if !strings.Contains(text, "id=10") || !strings.Contains(text, "id=3") {
		t.Errorf("原因列表缺 id：\n%s", text)
	}
}
