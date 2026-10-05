package membershipmcp

// membership_write_tools_test.go — 会员写工具的边界。
//
// 这一组里最要紧的三条都不是「参数校验」，而是**回执要说清后果**：
//   · 手工设级 = 接管（之后不再自动升级），不写出来用户不会想到要问；
//   · 取消锁定不立刻改级别（等日结），不写出来用户会以为按钮坏了；
//   · 权益必须带上力度 —— 只写 kind 的话「折扣」这条完全看不出是八折还是两折。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	membershipdto "go_wp/internal/module/membership/dto"
)

type stubMembershipWriter struct {
	assignReq *membershipdto.AssignManualReq
	assignRes *membershipdto.AssignmentResp
	assignErr error

	unlockReq *membershipdto.UnlockManualReq
	unlockErr error

	createReq *membershipdto.CreateTierReq
	createRes *membershipdto.TierResp
	createErr error

	updateReq *membershipdto.UpdateTierReq
	updateRes *membershipdto.TierResp
	updateErr error

	deleteReq *membershipdto.DeleteTierReq
	deleteErr error
}

func (s *stubMembershipWriter) AssignManual(_ context.Context, req *membershipdto.AssignManualReq) (*membershipdto.AssignmentResp, error) {
	s.assignReq = req
	return s.assignRes, s.assignErr
}

func (s *stubMembershipWriter) UnlockManual(_ context.Context, req *membershipdto.UnlockManualReq) error {
	s.unlockReq = req
	return s.unlockErr
}

func (s *stubMembershipWriter) ListTiers(context.Context, *membershipdto.ListTiersReq) ([]*membershipdto.TierResp, error) {
	return nil, nil
}

func (s *stubMembershipWriter) GetTier(context.Context, *membershipdto.GetTierReq) (*membershipdto.TierResp, error) {
	return nil, nil
}

func (s *stubMembershipWriter) CreateTier(_ context.Context, req *membershipdto.CreateTierReq) (*membershipdto.TierResp, error) {
	s.createReq = req
	return s.createRes, s.createErr
}

func (s *stubMembershipWriter) UpdateTier(_ context.Context, req *membershipdto.UpdateTierReq) (*membershipdto.TierResp, error) {
	s.updateReq = req
	return s.updateRes, s.updateErr
}

func (s *stubMembershipWriter) DeleteTier(_ context.Context, req *membershipdto.DeleteTierReq) error {
	s.deleteReq = req
	return s.deleteErr
}

func (s *stubMembershipWriter) SaveEntitlements(context.Context, *membershipdto.SaveEntitlementsReq) (*membershipdto.SaveEntitlementsResp, error) {
	return nil, nil
}

type memberWriteStore struct{ m map[string]mcp.Result }

func (s *memberWriteStore) Lookup(_ context.Context, tool, key string) (mcp.Result, bool, error) {
	r, ok := s.m[tool+"|"+key]
	return r, ok, nil
}

func (s *memberWriteStore) Save(_ context.Context, tool, key string, res mcp.Result) error {
	s.m[tool+"|"+key] = res
	return nil
}

func mustMemberWriteTools(t *testing.T, stub *stubMembershipWriter) map[string]mcp.Tool {
	t.Helper()
	tools, err := WriteTools(stub)
	if err != nil {
		t.Fatalf("装配会员写工具失败: %v", err)
	}
	out := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		out[tool.Name()] = tool
	}
	return out
}

func callMemberWrite(t *testing.T, stub *stubMembershipWriter, name string, args map[string]any) (string, error) {
	t.Helper()
	tool, ok := mustMemberWriteTools(t, stub)[name]
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

func TestMemberWriteRejectsNilDependency(t *testing.T) {
	if _, err := WriteTools(nil); err == nil {
		t.Fatal("writer 为 nil 应报错")
	}
}

func TestMemberWriteToolsAreFive(t *testing.T) {
	tools := mustMemberWriteTools(t, &stubMembershipWriter{})
	if len(tools) != 5 {
		t.Fatalf("应有 5 个写工具，实得 %d", len(tools))
	}
	for _, name := range []string{
		"member_assign_set", "member_assign_unlock",
		"member_tier_create", "member_tier_update", "member_tier_delete",
	} {
		if _, ok := tools[name]; !ok {
			t.Errorf("缺工具 %q", name)
		}
	}
}

// 设级的回执必须说清「已接管」：不回执这句话，用户不会知道自动升级从此对这个人失效。
func TestMemberAssignSetTextExplainsTakeover(t *testing.T) {
	stub := &stubMembershipWriter{assignRes: &membershipdto.AssignmentResp{
		UserID: 2, TierID: 3, TierName: "金卡", Source: "manual",
	}}
	text, err := callMemberWrite(t, stub, "member_assign_set", map[string]any{
		"projectId": "p-1", "userId": 2, "tierId": 3,
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"金卡", "接管", "不再随消费额自动重算", "member_assign_unlock"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
}

func TestMemberAssignSetMapsArgs(t *testing.T) {
	stub := &stubMembershipWriter{assignRes: &membershipdto.AssignmentResp{UserID: 2, TierID: 3, TierName: "金卡"}}
	if _, err := callMemberWrite(t, stub, "member_assign_set", map[string]any{
		"projectId": "p-1", "userId": 2, "tierId": 3,
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.assignReq.UserID != 2 || stub.assignReq.TierID != 3 || stub.assignReq.ProjectID != "p-1" {
		t.Errorf("映射错了: %+v", stub.assignReq)
	}
}

// 解锁的回执必须说清「等级不会立刻变」：不说的话用户会以为操作没生效。
func TestMemberAssignUnlockTextExplainsDelay(t *testing.T) {
	stub := &stubMembershipWriter{}
	text, err := callMemberWrite(t, stub, "member_assign_unlock", map[string]any{
		"projectId": "p-1", "userId": 2,
	})
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	for _, want := range []string{"交还自动管理", "下一次日结", "现在没变是正常的"} {
		if !strings.Contains(text, want) {
			t.Errorf("正文缺 %q：\n%s", want, text)
		}
	}
	// 不能报「当前等级是 X」—— UnlockManual 故意不动 tier_id，我们其实没读到等级名。
	if strings.Contains(text, "当前等级") {
		t.Errorf("不该声称知道当前等级：\n%s", text)
	}
}

// 门槛用「分」为单位直传，不做元→分换算：整数对整数，没有浮点误差的余地。
func TestMemberTierCreatePassesCentsVerbatim(t *testing.T) {
	stub := &stubMembershipWriter{createRes: &membershipdto.TierResp{ID: 3, Name: "金卡", SortOrder: 20, ThresholdAmount: 100000}}
	if _, err := callMemberWrite(t, stub, "member_tier_create", map[string]any{
		"projectId": "p-1", "name": "金卡", "sortOrder": 20, "thresholdCents": 100000,
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.createReq.ThresholdAmount != 100000 {
		t.Errorf("门槛应原样传 100000 分，实得 %d", stub.createReq.ThresholdAmount)
	}
}

func TestMemberTierCreatePassesEntitlements(t *testing.T) {
	stub := &stubMembershipWriter{createRes: &membershipdto.TierResp{ID: 3, Name: "金卡"}}
	if _, err := callMemberWrite(t, stub, "member_tier_create", map[string]any{
		"projectId": "p-1", "name": "金卡", "sortOrder": 20,
		"entitlements": []map[string]any{
			{"kind": "free_shipping", "valueInt": 1},
			{"kind": "discount", "valueInt": 20},
		},
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if len(stub.createReq.Entitlements) != 2 {
		t.Fatalf("权益没传全: %+v", stub.createReq.Entitlements)
	}
	if stub.createReq.Entitlements[1].ValueInt != 20 {
		t.Errorf("折扣力度映射错了: %+v", stub.createReq.Entitlements[1])
	}
}

func TestMemberTierCreateRejectsBadEntitlement(t *testing.T) {
	cases := []struct {
		name  string
		items []map[string]any
	}{
		{"折扣超范围", []map[string]any{{"kind": "discount", "valueInt": 101}}},
		{"折扣为零", []map[string]any{{"kind": "discount", "valueInt": 0}}},
		{"免运费取 2", []map[string]any{{"kind": "free_shipping", "valueInt": 2}}},
		{"未知类型", []map[string]any{{"kind": "coupon", "valueInt": 1}}},
	}
	for _, c := range cases {
		stub := &stubMembershipWriter{}
		if _, err := callMemberWrite(t, stub, "member_tier_create", map[string]any{
			"projectId": "p-1", "name": "金卡", "sortOrder": 20, "entitlements": c.items,
		}); err == nil {
			t.Errorf("%s：应被拒", c.name)
		}
		if stub.createReq != nil {
			t.Errorf("%s：被拒的请求不该到达 service", c.name)
		}
	}
}

func TestMemberTierUpdateRejectsEmptyChange(t *testing.T) {
	stub := &stubMembershipWriter{}
	if _, err := callMemberWrite(t, stub, "member_tier_update", map[string]any{
		"projectId": "p-1", "tierId": 3,
	}); err == nil {
		t.Fatal("五项全空应被拒")
	}
	if stub.updateReq != nil {
		t.Error("被拒的请求不该到达 service")
	}
}

// 空字符串不能透传成「改成空」：指针字段只该在显式传值时才有值。
func TestMemberTierUpdateOnlySendsProvidedFields(t *testing.T) {
	stub := &stubMembershipWriter{updateRes: &membershipdto.TierResp{ID: 3, Name: "金卡", ThresholdAmount: 200000}}
	if _, err := callMemberWrite(t, stub, "member_tier_update", map[string]any{
		"projectId": "p-1", "tierId": 3, "thresholdCents": 200000, "name": "",
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.updateReq.Name != nil {
		t.Errorf("空 name 不该带过去，实得 %q", *stub.updateReq.Name)
	}
	if stub.updateReq.ThresholdAmount == nil || *stub.updateReq.ThresholdAmount != 200000 {
		t.Errorf("门槛应带过去: %+v", stub.updateReq.ThresholdAmount)
	}
}

func TestMemberTierDeleteMapsArgs(t *testing.T) {
	stub := &stubMembershipWriter{}
	if _, err := callMemberWrite(t, stub, "member_tier_delete", map[string]any{
		"projectId": "p-1", "tierId": 3,
	}); err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if stub.deleteReq.TierID != 3 {
		t.Errorf("映射错了: %+v", stub.deleteReq)
	}
}

func TestMemberTierDeletePropagatesError(t *testing.T) {
	stub := &stubMembershipWriter{deleteErr: errors.New("该等级下还有会员")}
	if _, err := callMemberWrite(t, stub, "member_tier_delete", map[string]any{
		"projectId": "p-1", "tierId": 3,
	}); err == nil {
		t.Fatal("service 报错必须传出去（等级还被挂着时的拒绝要靠它）")
	}
}

// 权益正文必须带力度：只写 kind 的话「折扣」是八折还是两折看不出来。
func TestEntitlementsTextCarriesStrength(t *testing.T) {
	text := entitlementsText([]membershipdto.EntitlementResp{
		{Kind: "free_shipping", ValueInt: 1},
		{Kind: "discount", ValueInt: 20},
	})
	if !strings.Contains(text, "免运费") {
		t.Errorf("缺免运费：%s", text)
	}
	// 20 是扣减 20%（打 8 折），不是「20% 折扣」—— 两种说法都要出现，
	// 只给百分比会被读反，只给折扣数在非整数档（如 0.5 折）上又说不准。
	if !strings.Contains(text, "8 折") || !strings.Contains(text, "20%") {
		t.Errorf("折扣力度说不清：%s", text)
	}
}

func TestEntitlementsTextEmpty(t *testing.T) {
	if got := entitlementsText(nil); got != "" {
		t.Errorf("没有权益时不该造出内容，实得 %q", got)
	}
}

// 描述里必须写明「手工设级 = 接管」与门槛单位是分：
// 这两件事都不是模型能自己推出来的。
func TestMemberWriteDescriptionsStateKeySemantics(t *testing.T) {
	tools := mustMemberWriteTools(t, &stubMembershipWriter{})
	if desc := tools["member_assign_set"].Description(); !strings.Contains(desc, "接管") {
		t.Errorf("member_assign_set 描述没写清「接管」语义：\n%s", desc)
	}
	if desc := tools["member_tier_create"].Description(); !strings.Contains(desc, "分") {
		t.Errorf("member_tier_create 描述没说门槛单位：\n%s", desc)
	}
	if desc := tools["member_tier_create"].Description(); !strings.Contains(desc, "越大越高") {
		t.Errorf("member_tier_create 描述没说 sortOrder 的方向：\n%s", desc)
	}
}
