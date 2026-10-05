package ordermcp

// order_status_tools_test.go — 订单状态写工具的边界。
//
// 两个刻意的设计要在这里钉住：① 只吃 orderId，工程作用域由 service 探测；
// ② 操作人从 ctx 注入，不从参数读（状态日志是审计材料）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
)

type stubStatus struct {
	statusReq *orderdto.ChangeStatusReq
	cancelReq *orderdto.CancelOrderReq
	noteReq   *orderdto.UpdateOrderNoteReq
	noteRes   *orderdto.OrderResp
}

func (s *stubStatus) ChangeStatus(_ context.Context, req *orderdto.ChangeStatusReq) error {
	s.statusReq = req
	return nil
}

func (s *stubStatus) CancelOrder(_ context.Context, req *orderdto.CancelOrderReq) (*orderdto.CancelOrderResp, error) {
	s.cancelReq = req
	return &orderdto.CancelOrderResp{}, nil
}

func (s *stubStatus) UpdateOrderNote(_ context.Context, req *orderdto.UpdateOrderNoteReq) (*orderdto.OrderResp, error) {
	s.noteReq = req
	return s.noteRes, nil
}

func statusTool(t *testing.T, name string, s *stubStatus) mcp.Tool {
	t.Helper()
	tools, err := StatusWriteTools(s)
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	for _, tool := range tools {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("没有工具 %q", name)
	return mcp.Tool{}
}

func invokeStatus(t *testing.T, tool mcp.Tool, userID int64, args map[string]any) (string, error) {
	t.Helper()
	full := map[string]any{"confirm": true, "idempotencyKey": "k-" + t.Name()}
	for k, v := range args {
		full[k] = v
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	res, err := tool.Invoke(mcp.WithUserID(context.Background(), userID), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func TestStatusRejectsNilWriter(t *testing.T) {
	if _, err := StatusWriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestStatusToolNames(t *testing.T) {
	tools, err := StatusWriteTools(&stubStatus{})
	if err != nil {
		t.Fatalf("装配失败: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("应有 3 个工具，实得 %d", len(tools))
	}
	for _, want := range []string{"order_status", "order_cancel", "order_note"} {
		found := false
		for _, tool := range tools {
			if tool.Name() == want {
				found = true
			}
		}
		if !found {
			t.Errorf("缺工具 %s", want)
		}
	}
}

// 操作人身份必须来自 ctx，不能来自参数 —— 状态日志是审计材料。
func TestStatusInjectsOperatorFromContext(t *testing.T) {
	s := &stubStatus{}
	if _, err := invokeStatus(t, statusTool(t, "order_status", s), 42, map[string]any{
		"orderId": 7, "toStatus": "paid",
	}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.statusReq.OperatorID != 42 {
		t.Errorf("OperatorID 应来自 ctx（42），实得 %d", s.statusReq.OperatorID)
	}
	if s.statusReq.OperatorType != "admin" {
		t.Errorf("OperatorType 应为 admin，实得 %q", s.statusReq.OperatorType)
	}
}

// 参数里不该有 projectId —— 工程作用域由 service 探测。
func TestStatusSchemaHasNoProjectID(t *testing.T) {
	for _, name := range []string{"order_status", "order_cancel", "order_note"} {
		schema := fmt.Sprintf("%+v", statusTool(t, name, &stubStatus{}).Schema())
		if strings.Contains(schema, "projectId") {
			t.Errorf("%s 的 schema 不该出现 projectId（service 会自己探测，传错工程会踩 FORCE 策略）：\n%s", name, schema)
		}
		if !strings.Contains(schema, "orderId") {
			t.Errorf("%s 的 schema 应有 orderId：\n%s", name, schema)
		}
	}
}

// 取消和退款不走通用状态路径（取消要归还库存、退款要记流水号）。
func TestOrderStatusSchemaExcludesCancelAndRefund(t *testing.T) {
	schema := fmt.Sprintf("%+v", statusTool(t, "order_status", &stubStatus{}).Schema())
	for _, bad := range []string{`"cancelled"`, `"refunded"`} {
		if strings.Contains(schema, bad) {
			t.Errorf("order_status 不该接受 %s（各有专门入口）：\n%s", bad, schema)
		}
	}
	if !strings.Contains(strings.ToLower(statusTool(t, "order_status", &stubStatus{}).Description()), "order_cancel") {
		t.Error("order_status 的描述应把取消指向 order_cancel")
	}
}

func TestOrderStatusResultNamesLabel(t *testing.T) {
	s := &stubStatus{}
	text, err := invokeStatus(t, statusTool(t, "order_status", s), 1, map[string]any{
		"orderId": 7, "toStatus": "shipped", "remark": "顺丰 SF123",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "已发货") {
		t.Errorf("回执应用中文状态名，实得：%s", text)
	}
	if s.statusReq.Remark != "顺丰 SF123" {
		t.Errorf("remark 没传到: %+v", s.statusReq)
	}
}

// 取消回执必须说清「退款是另一步」—— 否则用户会以为取消就退了钱。
func TestOrderCancelResultExplainsRefundIsSeparate(t *testing.T) {
	s := &stubStatus{}
	text, err := invokeStatus(t, statusTool(t, "order_cancel", s), 1, map[string]any{
		"orderId": 7, "reason": "客户改主意",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.cancelReq.Reason != "客户改主意" {
		t.Errorf("reason 没传到: %+v", s.cancelReq)
	}
	if !strings.Contains(text, "退款") {
		t.Errorf("取消回执应说明退款是另一步，实得：%s", text)
	}
	if !strings.Contains(text, "库存") {
		t.Errorf("取消回执应说明库存已归还，实得：%s", text)
	}
}

// 备注是整段覆盖 —— 描述必须提醒先读。
func TestOrderNoteDescriptionWarnsOverwrite(t *testing.T) {
	desc := statusTool(t, "order_note", &stubStatus{}).Description()
	if !strings.Contains(desc, "order_get") {
		t.Errorf("备注描述应提醒先读原内容（整段覆盖）：\n%s", desc)
	}
	if !strings.Contains(desc, "覆盖") {
		t.Errorf("备注描述应点明是覆盖语义：\n%s", desc)
	}
}

func TestOrderNoteTextUsesOrderNo(t *testing.T) {
	s := &stubStatus{noteRes: &orderdto.OrderResp{OrderNo: "GWP2026100544A10858", AdminNote: "周五前发出"}}
	text, err := invokeStatus(t, statusTool(t, "order_note", s), 1, map[string]any{
		"orderId": 9, "adminNote": "周五前发出",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "GWP2026100544A10858") {
		t.Errorf("回执应用订单号（人认得的那个），实得：%s", text)
	}
}

func TestOrderNoteTextReportsClear(t *testing.T) {
	s := &stubStatus{noteRes: &orderdto.OrderResp{OrderNo: "GWP1", AdminNote: "  "}}
	text, err := invokeStatus(t, statusTool(t, "order_note", s), 1, map[string]any{
		"orderId": 9, "adminNote": "  ",
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "清空") {
		t.Errorf("空白备注应报「已清空」而不是「现在是：」，实得：%s", text)
	}
}
