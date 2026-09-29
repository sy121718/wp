package userhttp

// customer_membership_render_test.go — 客户详情页「会员等级」块的渲染证据（BIZ-3 展示侧）。
//
// 这一块的失效方向是**静默**的：会员模块没接入、等级读不出来、工程没选，
// 三种情况都不会报错 —— 页面上少一块、或者渲染中断成通用错误页。
// 所以断言既看「有数据时长什么样」，也看「三种降级各自的那一句话」与响应完整性。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
)

// stubCustomerMembership 会员身份读取端口的替身。
type stubCustomerMembership struct {
	res *membershipdto.MembershipResp
	err error
}

func (s *stubCustomerMembership) Resolve(_ context.Context, _ *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	return s.res, s.err
}

var _ membershipcontract.Reader = (*stubCustomerMembership)(nil)

// stubCustomerMembershipFacing 文案出口的替身。
type stubCustomerMembershipFacing struct{ text string }

func (s *stubCustomerMembershipFacing) FacingText(_ string, _ error) string { return s.text }

var _ membershipcontract.FacingTexter = (*stubCustomerMembershipFacing)(nil)

// detailBodyWithMembership 组装一个挂了会员端口的客户详情页并请求一次。
func detailBodyWithMembership(t *testing.T, reader membershipcontract.Reader, texter membershipcontract.FacingTexter) (int, string) {
	t.Helper()
	h := NewCustomerPageHandle(&fakeCustomerAdmin{detail: customerSample()}, fakeOrderSummaryReader{res: detailSummary()}, fakeProjects{items: detailProjects()})
	h.SetMembershipDisplay(reader, texter)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/detail?id=42", nil))
	return rec.Code, rec.Body.String()
}

// TestCustomerDetailShowsMembershipTier 有会员身份时把等级与权益摆到页面上。
func TestCustomerDetailShowsMembershipTier(t *testing.T) {
	code, body := detailBodyWithMembership(t, &stubCustomerMembership{res: &membershipdto.MembershipResp{
		UserID: 42, ProjectID: "p1", TierID: 3, TierName: "黄金会员",
		FreeShipping: true, DiscountPercent: 20,
	}}, nil)

	if code != http.StatusOK {
		t.Fatalf("客户详情页返回 %d", code)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatalf("响应不完整（缺 </html>）—— 模板渲染在中间某处中断了")
	}
	for _, want := range []string{"会员等级", "黄金会员", "免运费", "20%"} {
		if !strings.Contains(body, want) {
			t.Errorf("详情页缺少 %q", want)
		}
	}
}

// TestCustomerDetailShowsDefaultTierBadge 兜底等级要标明「还没有归属」。
//
// 运营看这一块是为了判断「该不该给他手工指定等级」—— 把兜底值画成真实等级，
// 这个判断就没有依据了。
func TestCustomerDetailShowsDefaultTierBadge(t *testing.T) {
	_, body := detailBodyWithMembership(t, &stubCustomerMembership{res: &membershipdto.MembershipResp{
		UserID: 42, ProjectID: "p1", TierID: 1, TierName: "普通会员", IsDefaultTier: true,
	}}, nil)

	if !strings.Contains(body, "普通会员") {
		t.Fatalf("缺少等级名")
	}
	if !strings.Contains(body, "还没有会员归属") {
		t.Fatalf("兜底等级应给出「还没有归属」的说明；实际：%s", body)
	}
}

// TestCustomerDetailMembershipUnavailableIsVisible 端口未接入 → 可见文案 + 页面完整。
func TestCustomerDetailMembershipUnavailableIsVisible(t *testing.T) {
	code, body := detailBodyWithMembership(t, nil, nil)

	if code != http.StatusOK {
		t.Fatalf("端口未接入时返回 %d（会员是只读展示，不该把整页打红）", code)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatal("响应不完整（缺 </html>）")
	}
	if !strings.Contains(body, "会员模块尚未接入") {
		t.Fatalf("端口未接入应给可见文案；实际：%s", body)
	}
}

// TestCustomerDetailMembershipFailureUsesFacingTexter 解析失败走文案出口，不漏内部原文。
func TestCustomerDetailMembershipFailureUsesFacingTexter(t *testing.T) {
	code, body := detailBodyWithMembership(t,
		&stubCustomerMembership{err: errors.New(`pq: relation "membership_tiers" does not exist`)},
		&stubCustomerMembershipFacing{text: "该工程还没有默认等级，请先建一个"})

	if code != http.StatusOK {
		t.Fatalf("解析失败时返回 %d", code)
	}
	if !strings.Contains(body, "该工程还没有默认等级") {
		t.Fatalf("应显示契约文案出口的那句话；实际：%s", body)
	}
	if strings.Contains(body, "membership_tiers") {
		t.Fatalf("内部错误原文泄漏到页面上：%s", body)
	}
}

// TestCustomerDetailMembershipWithoutProjectIsVisible 没有工程 → 说明等级按工程算。
func TestCustomerDetailMembershipWithoutProjectIsVisible(t *testing.T) {
	// fakeProjects 返回空清单 → selected 为空 → 走「还没有站点工程」那一支。
	h := NewCustomerPageHandle(&fakeCustomerAdmin{detail: customerSample()}, fakeOrderSummaryReader{}, fakeProjects{})
	h.SetMembershipDisplay(&stubCustomerMembership{res: &membershipdto.MembershipResp{TierName: "黄金会员"}}, nil)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/detail?id=42", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("返回 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "还没有站点工程") {
		t.Fatalf("无工程时应说明原因；实际：%s", rec.Body.String())
	}
}
