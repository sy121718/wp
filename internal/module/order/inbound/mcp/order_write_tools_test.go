package ordermcp

// order_write_tools_test.go — 代客建单的入参校验与结果可读性。
//
// 这里最要紧的两条，都是「模型很容易做错、做错了现场看不出来」的：
//   · provisionGuestAccount 不传时必须落成**明确的 false**，不能把 nil 透给 service ——
//     nil 的语义是「调用方没表态，保持前台行为（下单即开户）」，沿用它会每次代客建单
//     都顺手替客户开一个账号并寄一封密码邮件，而界面上看不出这件事发生过；
//   · 金额只能由服务端算：参数里没有商品单价，也没放 DiscountTotal ——
//     放了它们，模型会拿它们去凑用户说的价，而 service 在无券时忽略、有券时以试算为准，
//     用户听到的却是「已经按他说的价下单了」。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
)

type stubOrder struct {
	got *orderdto.CreateOrderReq
	res *orderdto.CreateOrderResp
	err error
}

func (s *stubOrder) CreateAdminOrder(_ context.Context, req *orderdto.CreateOrderReq) (*orderdto.CreateOrderResp, error) {
	s.got = req
	return s.res, s.err
}

func mustOrderWriteTools(t *testing.T, stub *stubOrder) map[string]mcp.Tool {
	t.Helper()
	tools, err := WriteTools(stub, newStore())
	if err != nil {
		t.Fatalf("装配写工具失败: %v", err)
	}
	out := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = tool
	}
	return out
}

// newStore 内存幂等台账（与真实实现同语义：只记成功结果）。
func newStore() *memStore { return &memStore{m: map[string]mcp.Result{}} }

type memStore struct{ m map[string]mcp.Result }

func (s *memStore) Lookup(_ context.Context, tool, key string) (mcp.Result, bool, error) {
	r, ok := s.m[tool+"|"+key]
	return r, ok, nil
}

func (s *memStore) Save(_ context.Context, tool, key string, res mcp.Result) error {
	s.m[tool+"|"+key] = res
	return nil
}

func callOrderCreate(t *testing.T, stub *stubOrder, args map[string]any) (string, error) {
	t.Helper()
	tool := mustOrderWriteTools(t, stub)["order_create"]
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	res, err := tool.Invoke(context.Background(), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func baseOrderArgs() map[string]any {
	return map[string]any{
		"projectId":     "p-1",
		"customerEmail": "buyer@example.com",
		"items":         []map[string]any{{"variantId": "v-1", "quantity": 2}},
	}
}

func TestOrderWriteToolsRejectNilDependency(t *testing.T) {
	if _, err := WriteTools(nil, newStore()); err == nil {
		t.Fatal("writer 为 nil 应报错（装配期接线缺陷要在启动时炸掉）")
	}
}

func TestOrderCreateMapsArgs(t *testing.T) {
	stub := &stubOrder{res: &orderdto.CreateOrderResp{ID: 7, OrderNo: "20261005001", Status: "pending", Total: 12300, Currency: "CNY"}}
	args := baseOrderArgs()
	args["customerName"] = "张三"
	args["shippingTotal"] = 500
	args["couponCode"] = "WELCOME"
	args["adminNote"] = "电话确认过"
	args["shipping"] = map[string]any{"name": "张三", "city": "杭州", "address": "文一西路 1 号", "country": "CN"}
	if _, err := callOrderCreate(t, stub, args); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.got == nil {
		t.Fatal("没有透传到 service")
	}
	if stub.got.ShippingTotal != 500 || stub.got.CouponCode != "WELCOME" || stub.got.AdminNote != "电话确认过" {
		t.Errorf("字段没传对: %+v", stub.got)
	}
	if stub.got.Shipping.City != "杭州" || stub.got.Shipping.Country != "CN" {
		t.Errorf("地址没传对: %+v", stub.got.Shipping)
	}
	if len(stub.got.Items) != 1 || stub.got.Items[0].VariantID != "v-1" || stub.got.Items[0].Quantity != 2 {
		t.Errorf("商品行没传对: %+v", stub.got.Items)
	}
}

// 不传 provisionGuestAccount 时必须落成明确的 false，不能是 nil。
func TestOrderCreateDefaultsGuestAccountToFalse(t *testing.T) {
	stub := &stubOrder{res: &orderdto.CreateOrderResp{OrderNo: "X"}}
	if _, err := callOrderCreate(t, stub, baseOrderArgs()); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.got.ProvisionGuestAccount == nil {
		t.Fatal("不能把 nil 透给 service：nil 的语义是「保持前台行为（下单即开户）」")
	}
	if *stub.got.ProvisionGuestAccount {
		t.Error("不传时应当是不开号")
	}
}

func TestOrderCreatePassesExplicitTrue(t *testing.T) {
	stub := &stubOrder{res: &orderdto.CreateOrderResp{OrderNo: "X"}}
	args := baseOrderArgs()
	args["provisionGuestAccount"] = true
	if _, err := callOrderCreate(t, stub, args); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.got.ProvisionGuestAccount == nil || !*stub.got.ProvisionGuestAccount {
		t.Error("显式传 true 时应当开号")
	}
}

func TestOrderCreateRejectsBadItems(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
	}{
		{"空 lines", map[string]any{"projectId": "p", "customerEmail": "a@b.c", "items": []map[string]any{}}},
		{"缺 variantId", map[string]any{"projectId": "p", "customerEmail": "a@b.c", "items": []map[string]any{{"quantity": 1}}}},
		{"数量为零", map[string]any{"projectId": "p", "customerEmail": "a@b.c", "items": []map[string]any{{"variantId": "v", "quantity": 0}}}},
		{"数量为负", map[string]any{"projectId": "p", "customerEmail": "a@b.c", "items": []map[string]any{{"variantId": "v", "quantity": -1}}}},
		{"负运费", map[string]any{"projectId": "p", "customerEmail": "a@b.c", "shippingTotal": -1, "items": []map[string]any{{"variantId": "v", "quantity": 1}}}},
	}
	for _, c := range cases {
		stub := &stubOrder{}
		if _, err := callOrderCreate(t, stub, c.args); err == nil {
			t.Errorf("%s：应当报错", c.name)
		}
		if stub.got != nil {
			t.Errorf("%s：被拒的请求不该到达 service", c.name)
		}
	}
}

func TestOrderCreateRejectsTooManyItems(t *testing.T) {
	items := make([]map[string]any, 0, orderCreateMaxItems+1)
	for i := 0; i <= orderCreateMaxItems; i++ {
		items = append(items, map[string]any{"variantId": "v", "quantity": 1})
	}
	stub := &stubOrder{}
	if _, err := callOrderCreate(t, stub, map[string]any{
		"projectId": "p", "customerEmail": "a@b.c", "items": items,
	}); err == nil {
		t.Errorf("超过 %d 行应报错", orderCreateMaxItems)
	}
}

func TestOrderCreatePropagatesServiceError(t *testing.T) {
	stub := &stubOrder{err: errors.New("库存不足")}
	if _, err := callOrderCreate(t, stub, baseOrderArgs()); err == nil {
		t.Fatal("service 报错必须传出去")
	}
}

func TestOrderCreateTextCarriesOrderNoAndMoney(t *testing.T) {
	stub := &stubOrder{res: &orderdto.CreateOrderResp{
		ID: 7, OrderNo: "20261005001", Status: "pending", Total: 12300, Currency: "CNY",
	}}
	text, err := callOrderCreate(t, stub, baseOrderArgs())
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	// 订单号是用户去后台找这一单的唯一钥匙，必须出现。
	for _, want := range []string{"20261005001", "CNY 123.00", "库存"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
}

// 幂等命中时要说清「这单不是新落的」：不说的话用户会以为重复下单了，
// 而真相多半是他自己点了两次、系统正确地去重了。
func TestOrderCreateTextMarksDuplicated(t *testing.T) {
	stub := &stubOrder{res: &orderdto.CreateOrderResp{
		ID: 7, OrderNo: "20261005001", Status: "pending", Total: 100, Currency: "CNY", Duplicated: true,
	}}
	text, err := callOrderCreate(t, stub, baseOrderArgs())
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !strings.Contains(text, "已经存在") {
		t.Errorf("幂等命中要明确说出来：\n%s", text)
	}
}

// 开号成功必须提示去收信 —— 不提示的话客户不知道自己已经有账号了。
func TestOrderCreateTextMentionsMailedAccount(t *testing.T) {
	stub := &stubOrder{res: &orderdto.CreateOrderResp{
		ID: 7, OrderNo: "X", Status: "pending", Total: 100, Currency: "CNY", AccountMailed: true,
	}}
	text, err := callOrderCreate(t, stub, baseOrderArgs())
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if !strings.Contains(text, "初始密码") {
		t.Errorf("开号后要告诉用户去收密码邮件：\n%s", text)
	}
}

// 参数里不能出现商品单价 / 折扣额：给了它们模型会拿去凑价，
// 而 service 在无券时忽略、有券时以试算为准。
func TestOrderCreateSchemaHasNoPriceFields(t *testing.T) {
	tool := mustOrderWriteTools(t, &stubOrder{})["order_create"]
	raw, err := tool.SchemaJSON()
	if err != nil {
		t.Fatalf("取 schema 失败: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("解析 schema 失败: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	for _, forbidden := range []string{"subtotal", "total", "discountTotal", "price", "unitPrice"} {
		if _, ok := props[forbidden]; ok {
			t.Errorf("参数里不该有 %q —— 定价是服务端的事", forbidden)
		}
	}
	// 运费是唯一的例外（运费策略不属于商品域），它必须在。
	if _, ok := props["shippingTotal"]; !ok {
		t.Error("缺 shippingTotal：运费是唯一允许调用方给出的金额字段")
	}
}
