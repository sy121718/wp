package userhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/pkg/i18n"
)

type stubCustomerMembership struct {
	res *membershipdto.MembershipResp
	err error
}

func (s *stubCustomerMembership) Resolve(_ context.Context, _ *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	return s.res, s.err
}

var _ membershipcontract.Reader = (*stubCustomerMembership)(nil)

func detailBodyWithMembership(t *testing.T, reader membershipcontract.Reader) (int, string) {
	t.Helper()
	h := NewCustomerPageHandle(&fakeCustomerAdmin{detail: customerSample()}, fakeOrderSummaryReader{res: detailSummary()}, fakeProjects{items: detailProjects()})
	h.SetMembershipDisplay(reader)
	engine := newCustomerTestEngine(h)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/detail?id=42", nil))
	return rec.Code, rec.Body.String()
}

func TestCustomerDetailShowsMembershipTier(t *testing.T) {
	code, body := detailBodyWithMembership(t, &stubCustomerMembership{res: &membershipdto.MembershipResp{
		UserID: 42, ProjectID: "p1", TierID: 3, TierName: "黄金会员",
		FreeShipping: true, DiscountPercent: 20,
	}})

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

func TestCustomerDetailShowsDefaultTierBadge(t *testing.T) {
	_, body := detailBodyWithMembership(t, &stubCustomerMembership{res: &membershipdto.MembershipResp{
		UserID: 42, ProjectID: "p1", TierID: 1, TierName: "普通会员", IsDefaultTier: true,
	}})

	if !strings.Contains(body, "普通会员") {
		t.Fatalf("缺少等级名")
	}
	if !strings.Contains(body, "还没有会员归属") {
		t.Fatalf("兜底等级应给出「还没有归属」的说明；实际：%s", body)
	}
}

func TestCustomerDetailMembershipUnavailableIsVisible(t *testing.T) {
	code, body := detailBodyWithMembership(t, nil)

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

func TestCustomerDetailMembershipFailureShowsDeclaredText(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		membershipenums.ErrDefaultTierMissing: {"zh-CN": "该工程还没有默认等级，请先建一个（错误里带的工程 id 用于定位）"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	code, body := detailBodyWithMembership(t,
		&stubCustomerMembership{err: membershipenums.Err(membershipenums.ErrDefaultTierMissing)})

	if code != http.StatusOK {
		t.Fatalf("解析失败时返回 %d", code)
	}
	if !strings.Contains(body, "该工程还没有默认等级") {
		t.Fatalf("应显示错误声明处的兜底文案；实际：%s", body)
	}
	if strings.Contains(body, "membership_tiers") {
		t.Fatalf("内部错误原文泄漏到页面上：%s", body)
	}
}

func TestCustomerDetailMembershipWithoutProjectIsVisible(t *testing.T) {

	h := NewCustomerPageHandle(&fakeCustomerAdmin{detail: customerSample()}, fakeOrderSummaryReader{}, fakeProjects{})
	h.SetMembershipDisplay(&stubCustomerMembership{res: &membershipdto.MembershipResp{TierName: "黄金会员"}})
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
