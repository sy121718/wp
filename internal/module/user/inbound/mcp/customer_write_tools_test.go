package usermcp

// customer_write_tools_test.go — 客户账号状态变更的边界。
//
// 两条最要紧的断言：
//   · status 不接受空串 / 未知值 —— 零值在这里的含义是「停用」，把没给的参数当 0
//     处理会把一次手滑变成一次停用；
//   · 解锁回执必须把 Unlocked 与 Cleared 分开说 —— 运营点了个不会变化的按钮时
//     需要知道的是「它本来就没锁」，否则他下一次还会再来点一遍。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
)

type stubCustomerWriter struct {
	statusReq *userdto.CustomerStatusReq
	statusRes *userdto.CustomerStatusResp
	statusErr error

	unlockReq *userdto.CustomerUnlockReq
	unlockRes *userdto.CustomerUnlockResp
	unlockErr error
}

func (s *stubCustomerWriter) SetCustomerStatus(_ context.Context, req *userdto.CustomerStatusReq) (*userdto.CustomerStatusResp, error) {
	s.statusReq = req
	return s.statusRes, s.statusErr
}

func (s *stubCustomerWriter) UnlockCustomer(_ context.Context, req *userdto.CustomerUnlockReq) (*userdto.CustomerUnlockResp, error) {
	s.unlockReq = req
	return s.unlockRes, s.unlockErr
}

type customerWriteStore struct{ m map[string]mcp.Result }

func (s *customerWriteStore) Lookup(_ context.Context, tool, key string) (mcp.Result, bool, error) {
	r, ok := s.m[tool+"|"+key]
	return r, ok, nil
}

func (s *customerWriteStore) Save(_ context.Context, tool, key string, res mcp.Result) error {
	s.m[tool+"|"+key] = res
	return nil
}

func mustCustomerWriteTools(t *testing.T, stub *stubCustomerWriter) map[string]mcp.Tool {
	t.Helper()
	tools, err := WriteTools(stub)
	if err != nil {
		t.Fatalf("装配客户写工具失败: %v", err)
	}
	out := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = tool
	}
	return out
}

func callCustomerWrite(t *testing.T, stub *stubCustomerWriter, name string, args map[string]any) (string, error) {
	t.Helper()
	tool, ok := mustCustomerWriteTools(t, stub)[name]
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

func TestCustomerWriteRejectsNilDependency(t *testing.T) {
	if _, err := WriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestCustomerWriteToolsAreExactlyTwo(t *testing.T) {
	tools := mustCustomerWriteTools(t, &stubCustomerWriter{})
	if len(tools) != 2 {
		t.Fatalf("应只有 2 个写工具，实得 %d", len(tools))
	}
	for _, name := range []string{"customer_set_status", "customer_unlock"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("缺工具 %q", name)
		}
	}
}

func TestCustomerSetStatusMapsActive(t *testing.T) {
	stub := &stubCustomerWriter{statusRes: &userdto.CustomerStatusResp{CustomerID: 7, Status: userenums.StatusActive}}
	if _, err := callCustomerWrite(t, stub, "customer_set_status", map[string]any{
		"customerId": 7, "status": "active",
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.statusReq.Status != userenums.StatusActive || stub.statusReq.CustomerID != 7 {
		t.Errorf("映射错了: %+v", stub.statusReq)
	}
}

func TestCustomerSetStatusMapsDisabled(t *testing.T) {
	stub := &stubCustomerWriter{statusRes: &userdto.CustomerStatusResp{CustomerID: 7, Status: userenums.StatusDisabled}}
	if _, err := callCustomerWrite(t, stub, "customer_set_status", map[string]any{
		"customerId": 7, "status": "disabled",
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.statusReq.Status != userenums.StatusDisabled {
		t.Errorf("映射错了: %+v", stub.statusReq)
	}
}

// 空串 / 未知状态必须当场拒：零值在落库侧是「停用」，透传等于手滑即停用。
func TestCustomerSetStatusRejectsUnknownStatus(t *testing.T) {
	for _, bad := range []string{"", "normal", "ban", "0"} {
		stub := &stubCustomerWriter{}
		if _, err := callCustomerWrite(t, stub, "customer_set_status", map[string]any{
			"customerId": 7, "status": bad,
		}); err == nil {
			t.Errorf("status=%q 应被拒", bad)
		}
		if stub.statusReq != nil {
			t.Errorf("status=%q：被拒的请求不该到达 service", bad)
		}
	}
}

func TestCustomerSetStatusRejectsZeroCustomerID(t *testing.T) {
	stub := &stubCustomerWriter{}
	if _, err := callCustomerWrite(t, stub, "customer_set_status", map[string]any{
		"customerId": 0, "status": "active",
	}); err == nil {
		t.Fatal("customerId=0 应被拒")
	}
}

// 停用的回执必须说清「他登不上」并给出恢复路径：
// 用户按下按钮后最常见的下一个问题是「我是不是弄错了」。
func TestCustomerSetStatusTextExplainsDisable(t *testing.T) {
	stub := &stubCustomerWriter{statusRes: &userdto.CustomerStatusResp{CustomerID: 7, Status: userenums.StatusDisabled}}
	text, err := callCustomerWrite(t, stub, "customer_set_status", map[string]any{
		"customerId": 7, "status": "disabled",
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"已停用", "登不上", "active"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
}

func TestCustomerSetStatusPropagatesError(t *testing.T) {
	stub := &stubCustomerWriter{statusErr: errors.New("用户不存在")}
	if _, err := callCustomerWrite(t, stub, "customer_set_status", map[string]any{
		"customerId": 7, "status": "active",
	}); err == nil {
		t.Fatal("service 报错必须传出去")
	}
}

// Unlocked 与 Cleared 是两件事，回执必须分开说。
func TestCustomerUnlockTextDistinguishesOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		res      *userdto.CustomerUnlockResp
		contains string
	}{
		{"两者都有", &userdto.CustomerUnlockResp{CustomerID: 7, Unlocked: true, Cleared: true}, "失败次数"},
		{"只解锁", &userdto.CustomerUnlockResp{CustomerID: 7, Unlocked: true}, "已解除登录锁定"},
		{"只清计数", &userdto.CustomerUnlockResp{CustomerID: 7, Cleared: true}, "并没有处于锁定状态"},
		{"什么都没做", &userdto.CustomerUnlockResp{CustomerID: 7}, "本来就没有锁定"},
	}
	for _, c := range cases {
		stub := &stubCustomerWriter{unlockRes: c.res}
		text, err := callCustomerWrite(t, stub, "customer_unlock", map[string]any{"customerId": 7})
		if err != nil {
			t.Fatalf("%s：不该报错 %v", c.name, err)
		}
		if !strings.Contains(text, c.contains) {
			t.Errorf("%s：正文缺 %q：\n%s", c.name, c.contains, text)
		}
	}
}

func TestCustomerUnlockRejectsZeroCustomerID(t *testing.T) {
	stub := &stubCustomerWriter{}
	if _, err := callCustomerWrite(t, stub, "customer_unlock", map[string]any{"customerId": 0}); err == nil {
		t.Fatal("customerId=0 应被拒")
	}
	if stub.unlockReq != nil {
		t.Error("被拒的请求不该到达 service")
	}
}

// 描述里必须写明「先核对身份、再让用户确认」：这是这两个工具唯一的行为约束，
// 而模型据描述决定怎么做。
func TestCustomerSetStatusDescriptionRequiresConfirmation(t *testing.T) {
	tool := mustCustomerWriteTools(t, &stubCustomerWriter{})["customer_set_status"]
	desc := tool.Description()
	for _, want := range []string{"customer_get", "确认"} {
		if !strings.Contains(desc, want) {
			t.Errorf("描述里缺 %q：\n%s", want, desc)
		}
	}
}
