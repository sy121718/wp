package ordermcp

// order_query_tools_test.go — 按线索查订单两个工具的元信息、入参透传与正文可读性。
//
// 正文断言不是「测文案」：模型的回答完全来自 Text（Data 只给渲染层），
// 正文里少了哪个数，用户就看不到哪个数。这里钉的是几件会直接决定回答对错的事：
//   · 单号必须逐条出现在结果里 —— 它是接着调 order_get 的唯一钥匙；
//   · 时间窗必须按**实际生效**的窗口回显，而不是把用户给的原串抄回来；
//   · 金额带币种、且币种取自订单（凭空写 ¥ 会在别的站点上答对数字、答错单位）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/pkg/utils"
)

// stubQuery 假 reader：记录入参、回放固定结果。
type stubQuery struct {
	gotFind *orderdto.FindOrderReq
	gotGet  *orderdto.GetOrderByNoReq

	findRes *orderdto.FindOrderResp
	getRes  *orderdto.OrderDetailResp
	err     error
}

func (s *stubQuery) FindOrders(_ context.Context, req *orderdto.FindOrderReq) (*orderdto.FindOrderResp, error) {
	s.gotFind = req
	return s.findRes, s.err
}

func (s *stubQuery) GetOrderDetailByNo(_ context.Context, req *orderdto.GetOrderByNoReq) (*orderdto.OrderDetailResp, error) {
	s.gotGet = req
	return s.getRes, s.err
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err != nil {
		t.Fatalf("解析时间失败: %v", err)
	}
	return parsed
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化入参失败: %v", err)
	}
	return b
}

func mustQueryTools(t *testing.T, stub *stubQuery) map[string]mcp.Tool {
	t.Helper()
	tools, err := QueryTools(stub)
	if err != nil {
		t.Fatalf("取查询工具集失败: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("按线索查订单应暴露 2 个工具，实得 %d", len(tools))
	}
	byName := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name()] = tool
	}
	return byName
}

func TestQueryToolsRejectsNilReader(t *testing.T) {
	if _, err := QueryTools(nil); err == nil {
		t.Fatal("依赖为 nil 应当报错（装配期接线缺陷要在启动时炸掉）")
	}
}

func TestQueryToolsMetadata(t *testing.T) {
	tools := mustQueryTools(t, &stubQuery{})
	for _, name := range []string{"order_find", "order_get"} {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("缺少工具 %s", name)
		}
		if strings.TrimSpace(tool.Description()) == "" {
			t.Errorf("%s 没有说明（模型靠它决定什么时候用这个工具）", name)
		}
	}
	// 关键承诺：模型得知道「一条线索就够了」，不必先猜用户给的是单号还是姓名。
	if !strings.Contains(tools["order_find"].Description(), "单号") ||
		!strings.Contains(tools["order_find"].Description(), "客户姓名") {
		t.Error("order_find 的说明应点明 keyword 同时匹配单号 / 邮箱 / 姓名")
	}
}

func TestQueryToolsRejectMissingRequiredArgs(t *testing.T) {
	tools := mustQueryTools(t, &stubQuery{})
	// 声明期就该拦下缺参：让请求走到 service 再报错，模型看到的是一个
	// 说不清原因的业务错误（「缺少工程」与「这单不存在」对它是一样的)。
	for _, tc := range []struct{ name, args string }{
		{"order_find", `{}`},
		{"order_find", `{"projectId":"p1","status":"not-a-status"}`},
		{"order_get", `{"projectId":"p1"}`},
	} {
		tool := tools[tc.name]
		var raw map[string]any
		_ = json.Unmarshal([]byte(tc.args), &raw)
		if _, err := tool.Invoke(context.Background(), mustRaw(t, raw)); err == nil {
			t.Errorf("%s 收到 %s 应当报错", tc.name, tc.args)
		}
	}
}

func TestFindOrdersTextListsOrderNoAndScope(t *testing.T) {
	stub := &stubQuery{findRes: &orderdto.FindOrderResp{
		From: "2026-10-01", To: "2026-10-05", Total: 7,
		List: []*orderdto.OrderResp{{
			OrderNo: "20261005001", Status: "shipped",
			CustomerName: "张三", CustomerEmail: "z@example.com",
			Total: 12345, Currency: "CNY",
			CreateTime: utils.NewJSONTime(mustTime(t, "2026-10-05 09:30:00")),
		}},
	}}
	tools := mustQueryTools(t, stub)
	raw := map[string]any{"projectId": "p1", "keyword": "张三", "from": "2026-10-01", "to": "2026-10-05"}
	res, err := tools["order_find"].Invoke(context.Background(), mustRaw(t, raw))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	// 入参必须原样透传（少传一个字段的表现是「筛了却没筛」）。
	if stub.gotFind == nil || stub.gotFind.Keyword != "张三" || stub.gotFind.From != "2026-10-01" {
		t.Fatalf("入参没有透传：%+v", stub.gotFind)
	}
	for _, want := range []string{"20261005001", "已发货", "张三", "z@example.com", "CNY 123.45", "2026-10-01 ~ 2026-10-05"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("正文缺少 %q：\n%s", want, res.Text)
		}
	}
	if res.Data == nil {
		t.Error("结构化数据应一并返回（渲染层要用它）")
	}
}

func TestFindOrdersTextExplainsEmptyResult(t *testing.T) {
	stub := &stubQuery{findRes: &orderdto.FindOrderResp{List: []*orderdto.OrderResp{}, Total: 0}}
	tools := mustQueryTools(t, stub)
	res, err := tools["order_find"].Invoke(context.Background(), json.RawMessage(`{"projectId":"p1"}`))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	// 空结果也要说清「按什么条件没找到」——只说「没有」时用户不知道是条件写错了
	// 还是真的没有，而这两件事的下一步完全不同。
	if !strings.Contains(res.Text, "没有符合条件的订单") || !strings.Contains(res.Text, "不限下单日期") {
		t.Errorf("空结果应带上实际生效的条件：\n%s", res.Text)
	}
}

func TestOrderDetailTextCarriesItemsAndLogs(t *testing.T) {
	stub := &stubQuery{getRes: &orderdto.OrderDetailResp{
		Head: &orderdto.OrderResp{
			OrderNo: "20261005001", Status: "shipped", CustomerName: "张三",
			Currency: "CNY", Subtotal: 20000, DiscountTotal: 1000, ShippingTotal: 500, Total: 19500,
			ShipName: "张三", ShipPhone: "13800000000", ShipProvince: "广东省", ShipCity: "深圳市",
			PaymentMethod: "wechat", PaymentMethodTitle: "微信支付",
			CreateTime: utils.NewJSONTime(mustTime(t, "2026-10-05 09:30:00")),
		},
		Items: []*orderdto.OrderItemResp{{
			ProductName: "香水", VariantLabel: "50ml", SKU: "TEO-50",
			UnitPrice: 10000, Quantity: 2, LineTotal: 20000,
		}},
		Logs: []*orderdto.StatusLogResp{
			{FromStatus: "pending", ToStatus: "paid", OperatorType: "customer", OperatorName: "张三",
				CreateTime: utils.NewJSONTime(mustTime(t, "2026-10-05 09:31:00"))},
			{FromStatus: "paid", ToStatus: "shipped", OperatorType: "admin", OperatorName: "李四",
				CreateTime: utils.NewJSONTime(mustTime(t, "2026-10-05 14:00:00"))},
		},
	}}
	tools := mustQueryTools(t, stub)
	res, err := tools["order_get"].Invoke(context.Background(), json.RawMessage(`{"projectId":"p1","orderNo":"20261005001"}`))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if stub.gotGet == nil || stub.gotGet.OrderNo != "20261005001" {
		t.Fatalf("入参没有透传：%+v", stub.gotGet)
	}
	for _, want := range []string{
		"20261005001", "已发货", "TEO-50", "香水 / 50ml",
		"广东省深圳市", "微信支付",
		// 流水是「到哪了」的答案本体：只有头部状态答不出时间与操作人。
		"待付款 → 已付款", "张三", "已付款 → 已发货", "李四", "管理员", "2026-10-05 14:00",
	} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("详情正文缺少 %q：\n%s", want, res.Text)
		}
	}
}

func TestQueryToolsPropagateServiceError(t *testing.T) {
	stub := &stubQuery{err: errors.New("数据库暂时不可用")}
	tools := mustQueryTools(t, stub)
	if _, err := tools["order_find"].Invoke(context.Background(), json.RawMessage(`{"projectId":"p1"}`)); err == nil {
		t.Error("service 的错误必须原样冒出来（吞掉的话模型会当成「查到了，是空的」）")
	}
}
