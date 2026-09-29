package runtimefragment

// membership_test.go — 会员片段的端点级测试（BIZ-3 消费侧接入）。
//
// 这一层守的是「降级必须可见」这件事：四个能力里任何一个走错分支，访客看到的都是
// **空白面板**（而且不报错、日志干净）：
//   · 未登录回 401 → htmx 默认不替换 401 的目标节点 → 面板毫无变化；
//   · 端口未接入 / 解析失败时返回 error → 片段端点变 500 → 同样不 swap。
// 所以下面的断言既看文案，也看**状态码**。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	usercontract "go_wp/internal/module/user/contract"
)

// stubMembershipReader 会员身份读取端口的替身。
type stubMembershipReader struct {
	res *membershipdto.MembershipResp
	err error
}

func (s *stubMembershipReader) Resolve(_ context.Context, _ *membershipdto.ResolveReq) (*membershipdto.MembershipResp, error) {
	return s.res, s.err
}

var _ membershipcontract.Reader = (*stubMembershipReader)(nil)

// stubFacingTexter 文案出口的替身（真实现是 membership service，测试里只关心「它被用到了」）。
type stubFacingTexter struct{ text string }

func (s *stubFacingTexter) FacingText(_ string, _ error) string { return s.text }

var _ membershipcontract.FacingTexter = (*stubFacingTexter)(nil)

// callMembershipFragment 调一次会员片段，返回（状态码, 响应体）。
func callMembershipFragment(t *testing.T, typeName, query string, visitorID uint64) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/_fragments/"+typeName+"?"+query, nil)
	c.Params = gin.Params{{Key: "type", Value: typeName}}
	if visitorID != 0 {
		c.Set(usercontract.VisitorContextKey, visitorID)
	}
	FragmentEndpoint(c)
	return w.Code, w.Body.String()
}

// withMembershipPorts 临时注入端口并在测试结束时还原（包级变量，测试之间不能互相污染）。
func withMembershipPorts(t *testing.T, reader membershipcontract.Reader, texter membershipcontract.FacingTexter) {
	t.Helper()
	prevReader, prevFacing := membershipReader, membershipFacing
	t.Cleanup(func() { membershipReader, membershipFacing = prevReader, prevFacing })
	SetMembershipReader(reader)
	SetMembershipFacingTexter(texter)
}

// TestMembershipFragmentsRenderTier 已登录 + 有等级：两个片段都渲染等级与权益。
func TestMembershipFragmentsRenderTier(t *testing.T) {
	withMembershipPorts(t, &stubMembershipReader{res: &membershipdto.MembershipResp{
		UserID: 42, ProjectID: "p-1", TierID: 2, TierName: "黄金会员",
		FreeShipping: true, DiscountPercent: 20,
	}}, nil)

	for _, typeName := range []string{membershipFragmentBadge, membershipFragmentPanel} {
		t.Run(typeName, func(t *testing.T) {
			code, body := callMembershipFragment(t, typeName, "projectId=p-1", 42)
			if code != http.StatusOK {
				t.Fatalf("状态码应为 200，实际 %d；体：%s", code, body)
			}
			if !strings.Contains(body, "黄金会员") {
				t.Fatalf("缺少等级名；实际：%s", body)
			}
		})
	}

	// 面板还要给出权益（免运费 / 折扣各一句）。
	_, panel := callMembershipFragment(t, membershipFragmentPanel, "projectId=p-1", 42)
	for _, want := range []string{"免运费", "20% 折扣"} {
		if !strings.Contains(panel, want) {
			t.Errorf("面板缺少权益 %q；实际：%s", want, panel)
		}
	}
}

// TestMembershipFragmentDefaultTierHint 兜底等级要说出「还不是会员」。
//
// 与「他是这个等级的会员」必须能分辨：运营/访客据此才知道该不该去攒消费。
func TestMembershipFragmentDefaultTierHint(t *testing.T) {
	withMembershipPorts(t, &stubMembershipReader{res: &membershipdto.MembershipResp{
		UserID: 42, ProjectID: "p-1", TierID: 1, TierName: "普通会员", IsDefaultTier: true,
	}}, nil)

	_, body := callMembershipFragment(t, membershipFragmentPanel, "projectId=p-1", 42)
	if !strings.Contains(body, "还不是会员") {
		t.Fatalf("兜底等级应给出升级引导；实际：%s", body)
	}
}

// TestMembershipFragmentGuestGetsPromptNot401 未登录给引导，且**不是 401**。
//
// 401 的后果不是「少一句话」：htmx 默认把 401 判成不替换，访客看到的是一个毫无变化的
// 面板，完全不知道要做什么（同 ordersList / accountPanel 的取舍）。
func TestMembershipFragmentGuestGetsPromptNot401(t *testing.T) {
	withMembershipPorts(t, &stubMembershipReader{res: &membershipdto.MembershipResp{TierName: "黄金会员"}}, nil)

	code, body := callMembershipFragment(t, membershipFragmentPanel, "projectId=p-1", 0)
	if code != http.StatusOK {
		t.Fatalf("未登录应返回 200 + 引导文案，实际 %d", code)
	}
	if code == http.StatusUnauthorized {
		t.Fatal("未登录返回 401：htmx 不会替换目标节点，访客什么都看不到")
	}
	if !strings.Contains(body, "登录后可以查看") {
		t.Fatalf("未登录应给登录引导；实际：%s", body)
	}
	if strings.Contains(body, "黄金会员") {
		t.Fatalf("未登录不该渲染任何等级信息；实际：%s", body)
	}
}

// TestMembershipFragmentWithoutPortIsVisible 端口未接入 → 可见文案 + 200。
func TestMembershipFragmentWithoutPortIsVisible(t *testing.T) {
	withMembershipPorts(t, nil, nil)

	code, body := callMembershipFragment(t, membershipFragmentPanel, "projectId=p-1", 42)
	if code != http.StatusOK {
		t.Fatalf("端口未接入应返回 200 + 可见文案，实际 %d", code)
	}
	if !strings.Contains(body, "会员信息暂时不可用") {
		t.Fatalf("端口未接入应给出可见文案；实际：%s", body)
	}
}

// TestMembershipFragmentMissingProjectIsVisible 页面没给工程 → 说清是配置问题。
//
// 与「未登录」分开：工程缺失是页面作者的问题，让访客去登录也修不好它。
func TestMembershipFragmentMissingProjectIsVisible(t *testing.T) {
	withMembershipPorts(t, &stubMembershipReader{res: &membershipdto.MembershipResp{TierName: "黄金会员"}}, nil)

	code, body := callMembershipFragment(t, membershipFragmentBadge, "", 42)
	if code != http.StatusOK {
		t.Fatalf("缺工程应返回 200 + 可见文案，实际 %d", code)
	}
	if !strings.Contains(body, "还没指定站点工程") {
		t.Fatalf("缺工程应说明原因；实际：%s", body)
	}
}

// TestMembershipFragmentResolveFailureUsesFacingTexter 解析失败走契约文案出口，不 500。
//
// err.Error() 直出会把 PostgreSQL 原文（表名 / 约束名）漏到访客页面上；
// 而返回 error 会让片段变 500（htmx 不 swap，面板毫无变化）。
func TestMembershipFragmentResolveFailureUsesFacingTexter(t *testing.T) {
	withMembershipPorts(t,
		&stubMembershipReader{err: errors.New(`pq: relation "membership_tiers" does not exist`)},
		&stubFacingTexter{text: "该工程还没有默认等级，请先在后台建一个"})

	code, body := callMembershipFragment(t, membershipFragmentPanel, "projectId=p-1", 42)
	if code != http.StatusOK {
		t.Fatalf("解析失败应返回 200 + 可见文案，实际 %d", code)
	}
	if !strings.Contains(body, "该工程还没有默认等级") {
		t.Fatalf("应使用契约文案出口的那句话；实际：%s", body)
	}
	if strings.Contains(body, "membership_tiers") {
		t.Fatalf("内部错误原文泄漏到响应里：%s", body)
	}
}

// TestMembershipFragmentWithoutFacingTexterFallsBack 没有文案出口时用本地兜底文案。
//
// 兜底文案**不是** err.Error()：装配缺一半时也不该把数据库原文漏出去。
func TestMembershipFragmentWithoutFacingTexterFallsBack(t *testing.T) {
	withMembershipPorts(t, &stubMembershipReader{err: errors.New(`pq: relation "membership_tiers" does not exist`)}, nil)

	code, body := callMembershipFragment(t, membershipFragmentBadge, "projectId=p-1", 42)
	if code != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", code)
	}
	if !strings.Contains(body, "会员信息暂时读不出来") {
		t.Fatalf("应给本地兜底文案；实际：%s", body)
	}
	if strings.Contains(body, "membership_tiers") {
		t.Fatalf("内部错误原文泄漏到响应里：%s", body)
	}
}

// TestMembershipFragmentsCloseTheirRoot 片段 HTML 完整（根节点闭合）。
//
// 半截 HTML 在 htmx 的 outerHTML 替换下会把容器结构吃掉 —— 页面看起来「坏了一半」，
// 而服务端既没报错也没有日志。
func TestMembershipFragmentsCloseTheirRoot(t *testing.T) {
	withMembershipPorts(t, &stubMembershipReader{res: &membershipdto.MembershipResp{TierName: "黄金会员"}}, nil)

	for _, tc := range []struct{ typeName, open, close string }{
		{membershipFragmentBadge, `<div class="sky-membership-badge" data-fragment="membershipBadge">`, `</div>`},
		{membershipFragmentPanel, `<section class="sky-membership-panel" data-fragment="membershipPanel">`, `</section>`},
	} {
		t.Run(tc.typeName, func(t *testing.T) {
			_, body := callMembershipFragment(t, tc.typeName, "projectId=p-1", 42)
			if !strings.Contains(body, tc.open) {
				t.Fatalf("缺少根节点 %q；实际：%s", tc.open, body)
			}
			if !strings.Contains(body, tc.close) {
				t.Fatalf("根节点未闭合（缺 %q）；实际：%s", tc.close, body)
			}
		})
	}
}
