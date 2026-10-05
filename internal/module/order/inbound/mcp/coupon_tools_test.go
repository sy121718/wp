package ordermcp

// coupon_tools_test.go / return_tools_test.go 的共同入口 —— 优惠券与退货两批
// 都在同一份测试文件里，因为它们的 schema 形状相似、判据也互相参照。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/mcp"
	orderdto "go_wp/internal/module/order/dto"
	"go_wp/pkg/utils"
)

type stubCouponStore struct{}

func (stubCouponStore) Lookup(context.Context, string, string) (mcp.Result, bool, error) {
	return mcp.Result{}, false, nil
}
func (stubCouponStore) Save(context.Context, string, string, mcp.Result) error { return nil }

type stubCouponRW struct {
	created *orderdto.CouponSaveReq
	updated *orderdto.CouponSaveReq
	deleted uint64
	listRes *orderdto.CouponListResp
	getRes  *orderdto.CouponResp
}

func (s *stubCouponRW) GetCoupon(context.Context, uint64) (*orderdto.CouponResp, error) {
	return s.getRes, nil
}
func (s *stubCouponRW) ListCoupons(context.Context, *orderdto.CouponListReq) (*orderdto.CouponListResp, error) {
	return s.listRes, nil
}
func (s *stubCouponRW) CreateCoupon(_ context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error) {
	s.created = req
	return s.getRes, nil
}
func (s *stubCouponRW) UpdateCoupon(_ context.Context, req *orderdto.CouponSaveReq) (*orderdto.CouponResp, error) {
	s.updated = req
	return s.getRes, nil
}
func (s *stubCouponRW) DeleteCoupon(_ context.Context, id uint64) error {
	s.deleted = id
	return nil
}

func couponTool(t *testing.T, name string, s *stubCouponRW) mcp.Tool {
	t.Helper()
	tools, err := CouponTools(s, s, stubCouponStore{})
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

func invoke(t *testing.T, tool mcp.Tool, args map[string]any, write bool) (string, error) {
	t.Helper()
	full := map[string]any{}
	for k, v := range args {
		full[k] = v
	}
	if write {
		full["confirm"] = true
		full["idempotencyKey"] = "k-" + t.Name()
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	res, err := tool.Invoke(mcp.WithUserID(context.Background(), 7), raw)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func TestCouponRejectsNilDeps(t *testing.T) {
	if _, err := CouponTools(nil, &stubCouponRW{}, stubCouponStore{}); err == nil {
		t.Fatal("读器 nil 应报错")
	}
	if _, err := CouponTools(&stubCouponRW{}, nil, stubCouponStore{}); err == nil {
		t.Fatal("写器 nil 应报错")
	}
	if _, err := CouponTools(&stubCouponRW{}, &stubCouponRW{}, nil); err == nil {
		t.Fatal("幂等存储 nil 应报错")
	}
}

// percent 是「减免的百分比」，不是「折扣力度」—— 差 10 倍，必须写在描述里。
func TestCouponPercentDescriptionSaysMinusPercent(t *testing.T) {
	desc := couponTool(t, "coupon_create", &stubCouponRW{}).Description()
	if !strings.Contains(desc, "减") || !strings.Contains(desc, "%") {
		t.Errorf("建券描述必须解释 percent 是减免百分比：\n%s", desc)
	}
	if !strings.Contains(desc, "分") {
		t.Errorf("建券描述必须说明 fixed 的单位是分：\n%s", desc)
	}
}

// 改券是整份覆盖 —— 描述必须强制先 get。
func TestCouponUpdateDescriptionRequiresGetFirst(t *testing.T) {
	desc := couponTool(t, "coupon_update", &stubCouponRW{}).Description()
	if !strings.Contains(desc, "coupon_get") {
		t.Errorf("改券描述必须要求先 coupon_get：\n%s", desc)
	}
	if !strings.Contains(desc, "覆盖") {
		t.Errorf("改券描述必须点明是整份覆盖：\n%s", desc)
	}
}

// 删券要先把人劝去停用 —— 停用可逆，删除不可逆。
func TestCouponDeletePushesDisableFirst(t *testing.T) {
	desc := couponTool(t, "coupon_delete", &stubCouponRW{}).Description()
	if !strings.Contains(desc, "停用") {
		t.Errorf("删券描述应先把人劝去停用：\n%s", desc)
	}
}

func TestCouponValueTextSaysMinusNotDiscount(t *testing.T) {
	got := couponValueText(&orderdto.CouponResp{DiscountType: "percent", DiscountValue: 90})
	if !strings.Contains(got, "减 90%") {
		t.Errorf("percent 90 应表述为「减 90%%」，实得 %q", got)
	}
	if strings.Contains(got, "九折") || strings.Contains(got, "0.9") {
		t.Errorf("不能表述成折扣力度，实得 %q", got)
	}
	if got2 := couponValueText(&orderdto.CouponResp{DiscountType: "fixed", DiscountValue: 500}); !strings.Contains(got2, "5.00") {
		t.Errorf("fixed 500 分应表述成 5.00 元，实得 %q", got2)
	}
}

// 券的「当前是否生效」由 State 决定，Status 只说明运营有没有手动停用。
func TestCouponStateTextUsesState(t *testing.T) {
	for state, want := range map[string]string{
		"enabled": "生效中", "disabled": "已停用", "expired": "已过期",
		"not_started": "未开始", "exhausted": "已用尽",
	} {
		if got := couponStateText(state); got != want {
			t.Errorf("state=%s 应为 %q，实得 %q", state, want, got)
		}
	}
}

func TestCouponCreatePassesFieldsThrough(t *testing.T) {
	s := &stubCouponRW{getRes: &orderdto.CouponResp{ID: 3, Code: "SUMMER", DiscountType: "percent", DiscountValue: 10}}
	if _, err := invoke(t, couponTool(t, "coupon_create", s), map[string]any{
		"projectId": "pr1", "name": "暑期活动", "discountType": "percent",
		"discountValue": 10, "minSubtotal": 10000, "maxUses": 100, "status": 1,
	}, true); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.created == nil {
		t.Fatal("没有调用 CreateCoupon")
	}
	if s.created.DiscountValue != 10 || s.created.MinSubtotal != 10000 || s.created.Status != 1 {
		t.Errorf("字段未透传: %+v", s.created)
	}
}

func TestCouponDeleteRequiresID(t *testing.T) {
	s := &stubCouponRW{}
	if _, err := invoke(t, couponTool(t, "coupon_delete", s), map[string]any{"couponId": 0}, true); err == nil {
		t.Fatal("couponId=0 应报错")
	}
	if s.deleted != 0 {
		t.Error("参数不合法时不该调用 service")
	}
}

// ---------------------------------------------------------------------------
// 退货

type stubReturnRW struct {
	approved *orderdto.ReturnReviewReq
	rejected *orderdto.ReturnReviewReq
	received *orderdto.ReturnReceiveReq
	resp     *orderdto.ReturnResp
	listRes  *orderdto.ReturnListResp
	detail   *orderdto.ReturnDetailResp
}

func (s *stubReturnRW) ListReturns(context.Context, *orderdto.ReturnListReq) (*orderdto.ReturnListResp, error) {
	return s.listRes, nil
}
func (s *stubReturnRW) GetReturn(context.Context, uint64) (*orderdto.ReturnDetailResp, error) {
	return s.detail, nil
}
func (s *stubReturnRW) ApproveReturn(_ context.Context, req *orderdto.ReturnReviewReq) (*orderdto.ReturnResp, error) {
	s.approved = req
	return s.resp, nil
}
func (s *stubReturnRW) RejectReturn(_ context.Context, req *orderdto.ReturnReviewReq) (*orderdto.ReturnResp, error) {
	s.rejected = req
	return s.resp, nil
}
func (s *stubReturnRW) ReceiveReturn(_ context.Context, req *orderdto.ReturnReceiveReq) (*orderdto.ReturnResp, error) {
	s.received = req
	return s.resp, nil
}

func returnTool(t *testing.T, name string, s *stubReturnRW) mcp.Tool {
	t.Helper()
	tools, err := ReturnTools(s, s, stubCouponStore{})
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

func TestReturnRejectsNilDeps(t *testing.T) {
	if _, err := ReturnTools(nil, &stubReturnRW{}, stubCouponStore{}); err == nil {
		t.Fatal("读器 nil 应报错")
	}
	if _, err := ReturnTools(&stubReturnRW{}, &stubReturnRW{}, nil); err == nil {
		t.Fatal("幂等存储 nil 应报错")
	}
}

// autoReceive=true 是一步到底（入库 + 退款）—— 描述与回执都必须说清。
func TestReturnApproveDescriptionExplainsAutoReceive(t *testing.T) {
	desc := returnTool(t, "return_approve", &stubReturnRW{}).Description()
	if !strings.Contains(desc, "autoReceive") {
		t.Errorf("描述必须点名 autoReceive：\n%s", desc)
	}
	if !strings.Contains(desc, "退款") || !strings.Contains(desc, "入库") {
		t.Errorf("描述必须说明一步到底会入库并退款：\n%s", desc)
	}
}

func TestReturnApproveTextDiffersByAutoReceive(t *testing.T) {
	s := &stubReturnRW{resp: &orderdto.ReturnResp{ID: 5, Status: "approved", StatusLabel: "已同意"}}
	text, err := invoke(t, returnTool(t, "return_approve", s), map[string]any{"returnId": 5}, true)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "return_receive") {
		t.Errorf("autoReceive=false 时应指向下一步收货：%s", text)
	}
	if strings.Contains(text, "钱已退") {
		t.Errorf("autoReceive=false 时不该说钱已退：%s", text)
	}

	s2 := &stubReturnRW{resp: &orderdto.ReturnResp{ID: 5, Status: "refunded", StatusLabel: "已退款"}}
	text2, err := invoke(t, returnTool(t, "return_approve", s2), map[string]any{"returnId": 5, "autoReceive": true}, true)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text2, "钱已退") {
		t.Errorf("autoReceive=true 时必须说明钱已退：%s", text2)
	}
}

// 驳回必须给理由 —— 客户会看到它。
func TestReturnRejectRequiresRemark(t *testing.T) {
	s := &stubReturnRW{resp: &orderdto.ReturnResp{ID: 5}}
	if _, err := invoke(t, returnTool(t, "return_reject", s), map[string]any{"returnId": 5}, true); err == nil {
		t.Fatal("缺 remark 应报错")
	}
	if s.rejected != nil {
		t.Error("参数不合法时不该调用 service")
	}
	text, err := invoke(t, returnTool(t, "return_reject", s), map[string]any{"returnId": 5, "remark": "超过 7 天"}, true)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(text, "超过 7 天") {
		t.Errorf("回执应复述给客户的理由：%s", text)
	}
}

// 操作人从 ctx 注入，不从参数读。
func TestReturnInjectsOperatorFromContext(t *testing.T) {
	s := &stubReturnRW{resp: &orderdto.ReturnResp{ID: 5}}
	if _, err := invoke(t, returnTool(t, "return_approve", s), map[string]any{"returnId": 5}, true); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.approved == nil || s.approved.OperatorID != 7 || s.approved.OperatorType != "admin" {
		t.Errorf("操作人应从 ctx 注入，实得 %+v", s.approved)
	}
}

func TestReturnReceiveInjectsOperator(t *testing.T) {
	s := &stubReturnRW{resp: &orderdto.ReturnResp{ID: 5}}
	if _, err := invoke(t, returnTool(t, "return_receive", s), map[string]any{"returnId": 5}, true); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if s.received == nil || s.received.OperatorID != 7 {
		t.Errorf("收货操作人应从 ctx 注入，实得 %+v", s.received)
	}
}

// 状态兜底不能只回裸码 —— 「已同意」与「已收货」在下游是两件不同的事。
func TestReturnStatusTextFallback(t *testing.T) {
	if got := returnStatusText("approved", ""); !strings.Contains(got, "待收货") {
		t.Errorf("approved 兜底应说明还没收货，实得 %q", got)
	}
	if got := returnStatusText("received", ""); !strings.Contains(got, "待退款") {
		t.Errorf("received 兜底应说明还没退款，实得 %q", got)
	}
	if got := returnStatusText("pending", "服务端标签"); got != "服务端标签" {
		t.Errorf("有服务端标签时应用它，实得 %q", got)
	}
}

// 详情里的明细挂在 Return.Items 上（ReturnDetailResp 是 {Return, Order} 组合）。
func TestReturnDetailReadsNestedItems(t *testing.T) {
	d := &orderdto.ReturnDetailResp{
		Return: &orderdto.ReturnResp{
			ID: 5, ReturnNo: "R1", OrderNo: "O1", Status: "pending",
			Reason: "尺码不对", RefundAmount: 12345,
			Items: []*orderdto.ReturnItemResp{{ProductName: "球鞋", Quantity: 1, RefundAmount: 12345}},
		},
	}
	text := returnDetailText(d)
	if !strings.Contains(text, "球鞋") || !strings.Contains(text, "123.45") {
		t.Errorf("详情应带出明细与金额：%s", text)
	}
	if !strings.Contains(text, "尺码不对") {
		t.Errorf("详情应带出申请理由：%s", text)
	}
}

// timeText 区分「不限」与「某个时刻」。
func TestCouponTimeTextNilMeansUnlimited(t *testing.T) {
	if got := timeText(nil); got != "不限" {
		t.Errorf("nil 应为「不限」，实得 %q", got)
	}
	tt := utils.JSONTime{}
	if got := timeText(&tt); got != "不限" {
		t.Errorf("零值时间应为「不限」，实得 %q", got)
	}
}
