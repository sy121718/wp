package inventorymcp

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/mcp"
	inventorydto "go_wp/internal/module/inventory/dto"
)

type stubReasonWriter struct {
	createReq *inventorydto.CreateReasonReq
	createRes *inventorydto.ReasonResp
	createErr error

	updateReq *inventorydto.UpdateReasonReq
	updateRes *inventorydto.ReasonResp
	updateErr error
}

func (s *stubReasonWriter) CreateReason(_ context.Context, req *inventorydto.CreateReasonReq) (*inventorydto.ReasonResp, error) {
	s.createReq = req
	return s.createRes, s.createErr
}

func (s *stubReasonWriter) UpdateReason(_ context.Context, req *inventorydto.UpdateReasonReq) (*inventorydto.ReasonResp, error) {
	s.updateReq = req
	return s.updateRes, s.updateErr
}

type reasonStore struct{ m map[string]mcp.Result }

func (s *reasonStore) Lookup(_ context.Context, tool, key string) (mcp.Result, bool, error) {
	r, ok := s.m[tool+"|"+key]
	return r, ok, nil
}

func (s *reasonStore) Save(_ context.Context, tool, key string, res mcp.Result) error {
	s.m[tool+"|"+key] = res
	return nil
}

func mustReasonTools(t *testing.T, stub *stubReasonWriter) map[string]mcp.Tool {
	t.Helper()
	tools, err := ReasonTools(stub)
	if err != nil {
		t.Fatalf("装配变动原因工具失败: %v", err)
	}
	out := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = tool
	}
	return out
}

func callReason(t *testing.T, stub *stubReasonWriter, name string, args map[string]any) (string, error) {
	t.Helper()
	tool, ok := mustReasonTools(t, stub)[name]
	if !ok {
		t.Fatalf("没有工具 %q", name)
	}
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

func TestReasonToolsRejectNilDependency(t *testing.T) {
	if _, err := ReasonTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestReasonToolsAreExactlyTwo(t *testing.T) {
	tools := mustReasonTools(t, &stubReasonWriter{})
	if len(tools) != 2 {
		t.Fatalf("应有 2 个工具，实得 %d", len(tools))
	}
	for _, name := range []string{"inventory_reason_create", "inventory_reason_update"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("缺工具 %q", name)
		}
	}
}

func TestReasonCreateMapsArgs(t *testing.T) {
	stub := &stubReasonWriter{createRes: &inventorydto.ReasonResp{Code: "store_pickup_out", Name: "门店自提出库", Direction: "out"}}
	if _, err := callReason(t, stub, "inventory_reason_create", map[string]any{
		"projectId": "p-1", "code": "store_pickup_out", "name": "门店自提出库", "direction": "out", "sort": 5,
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.createReq.Code != "store_pickup_out" || stub.createReq.Direction != "out" || stub.createReq.Sort != 5 {
		t.Errorf("映射错了: %+v", stub.createReq)
	}
}

func TestReasonCreateTextCarriesCodeVerbatim(t *testing.T) {
	stub := &stubReasonWriter{createRes: &inventorydto.ReasonResp{Code: "store_pickup_out", Name: "门店自提出库", Direction: "out"}}
	text, err := callReason(t, stub, "inventory_reason_create", map[string]any{
		"projectId": "p-1", "code": "store_pickup_out", "name": "门店自提出库", "direction": "out",
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"store_pickup_out", "门店自提出库", "出库", "reasonCode"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
}

func TestReasonCreateRejectsBadDirection(t *testing.T) {
	stub := &stubReasonWriter{}
	if _, err := callReason(t, stub, "inventory_reason_create", map[string]any{
		"projectId": "p-1", "code": "x", "name": "x", "direction": "sideways",
	}); err == nil {
		t.Fatal("非法方向应被拒")
	}
	if stub.createReq != nil {
		t.Error("被拒的请求不该到达 service")
	}
}

func TestReasonCreatePropagatesError(t *testing.T) {
	stub := &stubReasonWriter{createErr: errors.New("code 已存在")}
	if _, err := callReason(t, stub, "inventory_reason_create", map[string]any{
		"projectId": "p-1", "code": "purchase_in", "name": "采购", "direction": "in",
	}); err == nil {
		t.Fatal("service 报错必须传出去")
	}
}

func TestReasonUpdateRejectsEmptyChange(t *testing.T) {
	stub := &stubReasonWriter{}
	if _, err := callReason(t, stub, "inventory_reason_update", map[string]any{"reasonId": "7"}); err == nil {
		t.Fatal("三项全空应被拒")
	}
	if stub.updateReq != nil {
		t.Error("被拒的请求不该到达 service")
	}
}

func TestReasonUpdateOnlySendsProvidedFields(t *testing.T) {
	stub := &stubReasonWriter{updateRes: &inventorydto.ReasonResp{ID: "7", Name: "门店自提出库"}}
	if _, err := callReason(t, stub, "inventory_reason_update", map[string]any{
		"reasonId": "7", "status": "disabled", "name": "",
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.updateReq.Name != nil {
		t.Errorf("空 name 不该带过去，实得 %q", *stub.updateReq.Name)
	}
	if stub.updateReq.Status == nil || *stub.updateReq.Status != "disabled" {
		t.Errorf("status 应带过去: %+v", stub.updateReq.Status)
	}
}

func TestReasonUpdateTextExplainsDisable(t *testing.T) {
	stub := &stubReasonWriter{updateRes: &inventorydto.ReasonResp{ID: "7", Name: "门店自提出库"}}
	text, err := callReason(t, stub, "inventory_reason_update", map[string]any{
		"reasonId": "7", "status": "disabled",
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"已停用", "历史流水照常显示", "数据没有动"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
}

func TestReasonUpdatePropagatesError(t *testing.T) {
	stub := &stubReasonWriter{updateErr: errors.New("内置原因不能改名")}
	if _, err := callReason(t, stub, "inventory_reason_update", map[string]any{
		"reasonId": "7", "name": "新名字",
	}); err == nil {
		t.Fatal("service 报错必须传出去")
	}
}

func TestReasonUpdateDescriptionWarnsBuiltin(t *testing.T) {
	tool := mustReasonTools(t, &stubReasonWriter{})["inventory_reason_update"]
	if !strings.Contains(tool.Description(), "内置") {
		t.Errorf("描述里没提内置原因的限制：\n%s", tool.Description())
	}
}

func TestReasonCreateTextNeverLeaksI18nKey(t *testing.T) {
	stub := &stubReasonWriter{createRes: &inventorydto.ReasonResp{
		Code:      "store_pickup_out",
		Name:      "inventory.reason.custom.52935790-31b4-4bcd-8eb6-ccdc46b342c7.store_pickup_out",
		Direction: "out",
	}}
	text, err := callReason(t, stub, "inventory_reason_create", map[string]any{
		"projectId": "52935790-31b4-4bcd-8eb6-ccdc46b342c7",
		"code":      "store_pickup_out",
		"name":      "门店自提出库",
		"direction": "out",
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if strings.Contains(text, "inventory.reason.") {
		t.Errorf("回执里漏了裸 i18n key：\n%s", text)
	}

	if !strings.Contains(text, "store_pickup_out") {
		t.Errorf("正文缺 code：\n%s", text)
	}
}

func TestReasonUpdateTextNeverLeaksI18nKey(t *testing.T) {
	stub := &stubReasonWriter{updateRes: &inventorydto.ReasonResp{
		ID:   "10",
		Code: "store_pickup_out",
		Name: "inventory.reason.custom.52935790-31b4-4bcd-8eb6-ccdc46b342c7.store_pickup_out",
	}}
	text, err := callReason(t, stub, "inventory_reason_update", map[string]any{
		"reasonId": "10", "sort": 1,
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if strings.Contains(text, "inventory.reason.") {
		t.Errorf("回执里漏了裸 i18n key：\n%s", text)
	}
}
