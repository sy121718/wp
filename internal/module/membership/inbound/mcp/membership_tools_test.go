package membershipmcp

// membership_tools_test.go — 会员两个只读工具的元信息、入参边界与正文可读性。
//
// 正文断言不是「测文案」：模型的回答完全来自 Text（Data 只给渲染层）。
// 这里钉的是几件会直接决定回答对错的事：
//   · 门槛金额库里存的是**分**，正文必须换算成元 —— 直接念 50000 会让运营
//     以为门槛是五万元；
//   · auto（按累计消费自动升降）与 manual（后台手工设定）要分开说：
//     运营看到「这个人怎么是金卡」时，下一步动作完全不同；
//   · userId / tierId 的「不筛」是 0，必须与「筛 id=0」区分开。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go_wp/internal/mcp"
	membershipdto "go_wp/internal/module/membership/dto"
	"go_wp/pkg/utils"
)

type stubReader struct {
	gotTiers       *membershipdto.ListTiersReq
	gotAssignments *membershipdto.ListAssignmentsReq

	tierRes []*membershipdto.TierResp
	assnRes []*membershipdto.AssignmentResp
	err     error
}

func (s *stubReader) ListTiers(_ context.Context, req *membershipdto.ListTiersReq) ([]*membershipdto.TierResp, error) {
	s.gotTiers = req
	return s.tierRes, s.err
}

func (s *stubReader) ListAssignments(_ context.Context, req *membershipdto.ListAssignmentsReq) ([]*membershipdto.AssignmentResp, error) {
	s.gotAssignments = req
	return s.assnRes, s.err
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
		t.Fatalf("取会员工具集失败: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("会员工具应暴露 2 个，实得 %d", len(tools))
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

func TestToolNamesAreStable(t *testing.T) {
	tools := mustTools(t, &stubReader{})
	for _, name := range []string{"member_tiers", "member_find"} {
		if _, ok := tools[name]; !ok {
			t.Fatalf("缺少工具 %s", name)
		}
	}
}

func TestMemberTiersRequiresProject(t *testing.T) {
	stub := &stubReader{}
	tools := mustTools(t, stub)
	_, err := tools["member_tiers"].Invoke(context.Background(), mustRaw(t, map[string]any{}))
	if err == nil {
		t.Fatal("projectId 必填，缺失应被拦下")
	}
	var argsErr *mcp.ArgsError
	if !errors.As(err, &argsErr) {
		t.Fatalf("应是 *mcp.ArgsError，实得 %T（%v）", err, err)
	}
	if stub.gotTiers != nil {
		t.Fatal("参数不合规时不应打到 service")
	}
}

// 这条是关键：门槛单位是分，正文必须换算成元。
func TestTierTextConvertsCentsToYuan(t *testing.T) {
	text := tierListText([]*membershipdto.TierResp{
		{ID: 1, Name: "金卡", ThresholdAmount: 50000, SortOrder: 2},
		{ID: 2, Name: "普通会员", ThresholdAmount: 0, SortOrder: 1, IsDefault: true},
	})
	if !strings.Contains(text, "500.00 元") {
		t.Fatalf("门槛 50000 分应显示成 500.00 元（不能直接念原始数字）：\n%s", text)
	}
	if strings.Contains(text, "50000") {
		t.Fatalf("原始分值不该出现在正文里（会被读成五万元）：\n%s", text)
	}
	if !strings.Contains(text, "无门槛") {
		t.Fatalf("零门槛要明说（不是「0 元」）：\n%s", text)
	}
	if !strings.Contains(text, "默认等级") {
		t.Fatalf("默认等级要标出来（每工程恰一个，它决定了新人从哪起步）：\n%s", text)
	}
}

func TestTierTextEmpty(t *testing.T) {
	if got := tierListText(nil); !strings.Contains(got, "还没有会员等级") {
		t.Fatalf("空清单应直说：%s", got)
	}
}

func TestMemberFindPassesFilters(t *testing.T) {
	stub := &stubReader{assnRes: []*membershipdto.AssignmentResp{}}
	tools := mustTools(t, stub)
	_, err := tools["member_find"].Invoke(context.Background(), mustRaw(t, map[string]any{
		"projectId": "p1",
		"tierId":    7,
		"source":    "manual",
		"userId":    42,
		"limit":     5,
	}))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	got := stub.gotAssignments
	if got.ProjectID != "p1" || got.TierID != 7 || got.Source != "manual" || got.UserID != 42 {
		t.Fatalf("筛选条件应逐条透传，实得 %+v", got)
	}
	if got.Size != 5 || got.Page != 1 {
		t.Fatalf("limit 应透传、Page 恒为 1，实得 size=%d page=%d", got.Size, got.Page)
	}
}

func TestMemberFindRejectsUnknownSource(t *testing.T) {
	tools := mustTools(t, &stubReader{assnRes: []*membershipdto.AssignmentResp{}})
	if _, err := tools["member_find"].Invoke(context.Background(), mustRaw(t, map[string]any{"projectId": "p1", "source": "auto2"})); err == nil {
		t.Fatal("白名单之外的取值应被拦下（真实取值只有 auto / manual）")
	}
}

func TestMemberFindClampsLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, memberDefaultLimit},
		{-1, memberDefaultLimit},
		{4, 4},
		{999, memberMaxLimit},
	}
	for _, tc := range cases {
		stub := &stubReader{assnRes: []*membershipdto.AssignmentResp{}}
		tools := mustTools(t, stub)
		if _, err := tools["member_find"].Invoke(context.Background(), mustRaw(t, map[string]any{"projectId": "p1", "limit": tc.in})); err != nil {
			t.Fatalf("调用失败: %v", err)
		}
		if stub.gotAssignments.Size != tc.want {
			t.Fatalf("limit %d 应夹取成 %d，实得 %d", tc.in, tc.want, stub.gotAssignments.Size)
		}
	}
}

// auto（按消费自动升降）与 manual（后台手工设定）必须分开说。
func TestSourceTextDistinguishesAutoFromManual(t *testing.T) {
	auto := sourceText("auto")
	manual := sourceText("manual")
	if auto == manual {
		t.Fatalf("自动与手工必须分开说，实得都是 %q", auto)
	}
	if sourceText("") == "" {
		t.Fatal("空来源也要有说法（否则正文里会缺一格）")
	}
}

func TestAssignmentListTextCarriesTierAndSource(t *testing.T) {
	text := assignmentListText([]*membershipdto.AssignmentResp{
		{ID: 1, UserID: 42, TierID: 3, TierName: "金卡", Source: "auto", AssignedAt: utils.NewJSONTime(time.Now())},
		{ID: 2, UserID: 43, TierID: 3, TierName: "金卡", Source: "manual"},
	}, memberFindArgs{ProjectID: "p1", TierID: 3})
	for _, want := range []string{
		"客户 id=42", "金卡", "自动（按累计消费）",
		"客户 id=43", "手工设定",
		"等级 id=3", // 回显筛选范围，用户才知道自己筛了什么
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("正文里应含 %q：\n%s", want, text)
		}
	}
}

func TestAssignmentListTextEmptyExplainsScope(t *testing.T) {
	text := assignmentListText(nil, memberFindArgs{ProjectID: "p1", TierID: 9})
	if !strings.Contains(text, "没有符合条件") {
		t.Fatalf("空结果应直说：%s", text)
	}
	if !strings.Contains(text, "等级 id=9") {
		t.Fatalf("空结果要回显范围：%s", text)
	}
	if !strings.Contains(text, "member_tiers") {
		t.Fatalf("空结果要给出下一步（确认等级 id）：%s", text)
	}
}
