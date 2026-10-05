package inventorymcp

// stock_write_tools_test.go — 库存写工具的入参校验、错误教学与结果可读性。
//
// 写工具比读工具多一层要钉的东西：**它真的会改数据**。所以这里除了「正文里有没有
// 那个数」，还钉住几件会直接造成错误写入或让模型反复重试的事：
//   · in/out 的 quantity 必须是正数（负数会写成「入库 -5」，事后没人看得出它其实是出库）；
//   · adjust 允许 0（清零是常见盘点结果）但拒绝负数；
//   · reasonCode 被拒时错误里要带上该方向可用的 code —— 否则模型只会重猜一次，
//     同一次入库连着失败两三次，而用户看到的是「AI 试了半天没做成」；
//   · 结果必须写出变动前后的数量（只报增量的话用户还得再查一次才能回答「现在有多少」）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

// stubStore 幂等台账的内存实现：只记成功结果，与真实实现同语义。
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

// 写工具的 reader 参数是 StockReader（错误教学要用 ListReasons）——
// 把另外两个方法补成空实现，让同一个 stub 同时满足读写两侧。
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

// callStockChange 发起一次 stock_change，自动补上框架要求的 confirm + idempotencyKey。
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
	// 行内的仓库要跟着请求上的仓库走：只传顶层仓库、行里留空时，
	// service 会按行内的空值去找仓库 —— 那是一次「仓库不存在」的报错。
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

// reasonCode 被拒时要把该方向可用的 code 附在错误里 —— 这是本文件里最要紧的一条。
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

// 与原因无关的错误不该被塞一份原因清单：那会让人以为是原因的问题。
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

// 缓存没同步上必须说出来：列表页的库存数会短暂与真实值不一致，
// 用户拿列表页的数去对账会对不上。
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
	// 空结果必须给出下一步（去建一个），而不是让模型以为工具坏了。
	if !strings.Contains(res.Text, "建") {
		t.Errorf("空结果应告诉用户下一步：\n%s", res.Text)
	}
}

// 幂等：同一个 key 第二次调用应当拿回第一次的结果，而不是再写一次库存。
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

// 内置原因的 Name 是 i18n key，不能原样吐给用户。
//
// 实测踩到过：回答里出现「purchase_in — inventory.reason.purchase_in」。
// 这里钉住「内置 key 绝不原样出现」—— 翻译命中（生产环境）出中文，
// 未命中（单元测试里 i18n 缓存未加载）退回 code，两种都不该是点分 key。
func TestReasonNameNeverLeaksI18nKey(t *testing.T) {
	builtin := &inventorydto.ReasonResp{Code: "purchase_in", Name: "inventory.reason.purchase_in", IsBuiltin: true}
	got := reasonName(builtin)
	if strings.Contains(got, "inventory.reason.") {
		t.Errorf("内置原因名不该是裸 i18n key：%q", got)
	}
	if got != "purchase_in" && got != "采购入库" {
		t.Errorf("内置原因名应当是译文或 code，实得 %q", got)
	}
	// 自建原因的 Name 就是真名，必须原样保留。
	custom := &inventorydto.ReasonResp{Code: "my_reason", Name: "供应商送样"}
	if reasonName(custom) != "供应商送样" {
		t.Errorf("自建原因名应当原样保留，实得 %q", reasonName(custom))
	}
}

// 原因列表必须带出 id：inventory_reason_update 要的是 id 不是 code，
// 不印出来模型下一步只能停下来问用户要。
// 实测原话：「刚才 stock_reasons 返回里没给出它的 reasonId，我不能拿 code 代填」。
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
