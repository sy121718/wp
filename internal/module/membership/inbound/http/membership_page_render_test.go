package membershiphttp

import (
	pageview "go_wp/pkg/view"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/facing"
	"go_wp/pkg/pagination"
	"go_wp/pkg/utils"
)

func membershipRenderData(lang string) gin.H {
	return gin.H{
		"lang": lang, "t": templates.TranslateFunc(lang),
		"langs": templates.LanguageOptions(lang), "lang_redirect": "/admin/membership",
		"csrf_token": "test-token",
		"Buttons": map[string]bool{
			"membership.tier_create": true, "membership.tier_update": true,
			"membership.tier_delete": true, "membership.assign_set": true,
			"membership.assign_unlock": true,
		},
		"NavGroups": []any{}, "SidebarOpen": false, "SidebarPinned": false, "HasSubnav": false,
	}
}

func renderMembershipPage(t *testing.T, tpl string, over map[string]any) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	data := membershipRenderData("zh-CN")
	for k, v := range over {
		data[k] = v
	}
	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/membership", func(c *gin.Context) {
		c.HTML(http.StatusOK, tpl, data)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/membership", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "</html>") {
		t.Fatalf("响应不是完整页面（缺 </html>），长度 %d", len(body))
	}
	return body
}

func tiersPageFixture(over map[string]any) map[string]any {
	data := map[string]any{
		"title": "admin.membership.title", "menu": "membership",
		"Projects":        []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}},
		"SelectedProject": "p-1",
		"Tiers": []membershipTierRow{
			{ID: 1, Name: "普通会员", SortOrder: 0, ThresholdAmount: 0, IsDefault: true},
			{ID: 2, Name: "白银会员", SortOrder: 1, ThresholdAmount: 100000,
				FreeShipping: true, HasDiscount: true, DiscountPercent: 5, HasEntitlement: true},
		},
		"HasTiers":              true,
		"KindFreeShippingValue": "free_shipping", "KindDiscountValue": "discount",
		"ListQuery": "", "LoadErr": "", "LoadFailed": false,
	}
	for k, v := range over {
		data[k] = v
	}
	return data
}

func TestMembershipTiersPageRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(nil))
	for _, want := range []string{
		"会员等级与权益", "普通会员", "白银会员",

		"1000.00",

		"默认等级",

		"免运费", "5%",

		"tpl-tier-create",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}
}

func TestMembershipTiersPageConvertsCentsInTemplate(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Tiers": []membershipTierRow{
			{ID: 3, Name: "黄金会员", SortOrder: 2, ThresholdAmount: 100005,
				HasDiscount: true, DiscountPercent: 20, HasEntitlement: true},
		},
	}))
	if !strings.Contains(body, "1000.05") {
		t.Errorf("门槛应按分 → 元换算成 1000.05")
	}
	if strings.Contains(body, "100005") {
		t.Errorf("原始分数值不该出现在页面上（说明换算被漏掉了）")
	}
}

func TestMembershipTiersPageEditRegionRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Edit": &membershipTierEdit{
			ID: 2, Name: "白银会员", SortOrder: 1, ThresholdAmount: 100000,
			Remark: "满千升级", FreeShipping: true, HasDiscount: true, DiscountPercent: 5,
		},
	}))
	for _, want := range []string{
		"编辑等级", "白银会员", "满千升级",

		`name="thresholdYuan" value="1000.00"`,

		`name="discountPercent" value="5"`,

		"设为默认等级",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑区缺少 %q", want)
		}
	}
}

func TestMembershipTiersPageEditLeavesDiscountEmptyWithoutEntitlement(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Edit": &membershipTierEdit{ID: 1, Name: "普通会员", ThresholdAmount: 0, IsDefault: true},
	}))
	if !strings.Contains(body, `name="discountPercent" value=""`) {
		t.Errorf("没有折扣权益时应留空回填（不能是 0）")
	}
}

func TestMembershipTiersPageEmptyAndLoadFailed(t *testing.T) {
	empty := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Tiers": []membershipTierRow{}, "HasTiers": false,
	}))
	if !strings.Contains(empty, "这个工程还没有等级") {
		t.Errorf("空态文案缺失")
	}

	if strings.Contains(empty, "危险操作") {
		t.Errorf("没有 ?edit= 时不该渲染编辑区")
	}

	fallback := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Projects": []projectcontract.ProjectResp{}, "SelectedProject": "",
		"Tiers": []membershipTierRow{}, "HasTiers": false,
		"LoadFailed": true, "LoadErr": "工程列表没读出来",
	}))
	if !strings.Contains(fallback, "工程列表没读出来") {
		t.Errorf("降级分支的提示缺失")
	}
}

func assignmentsPageFixture(over map[string]any) map[string]any {
	data := map[string]any{
		"title": "admin.membership.assign.title", "menu": "membership-assignments",
		"Projects":        []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}},
		"SelectedProject": "p-1",
		"TierOptions": []gin.H{
			{"Value": int64(1), "Label": "普通会员", "Selected": false},
			{"Value": int64(2), "Label": "白银会员", "Selected": true},
		},
		"SourceOptions": []gin.H{
			{"Value": "", "Selected": true},
			{"Value": membershipmodel.SourceAuto, "Selected": false},
			{"Value": membershipmodel.SourceManual, "Selected": false},
		},
		"FilterTier": int64(0), "FilterSource": "", "FilterUser": uint64(0),
		"Rows": []membershipAssignRow{
			{ID: 9, UserID: 1024, TierID: 2, TierName: "白银会员",
				Source: membershipmodel.SourceManual, AssignedAt: membershipAssignTestTime()},
		},
		"Total":     int64(1),
		"ListQuery": "", "LoadErr": "", "LoadFailed": false, "RecalcAvailable": false,

		"Pagination": pagination.Page{},

		"SourceManual": membershipmodel.SourceManual,
		"SourceAuto":   membershipmodel.SourceAuto,
	}
	for k, v := range over {
		data[k] = v
	}
	return data
}

func membershipAssignTestTime() utils.JSONTime {
	return utils.JSONTime(time.Date(2026, 9, 26, 10, 0, 0, 0, time.Local))
}

func TestMembershipAssignmentsPageRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership_assignments.html", assignmentsPageFixture(nil))
	for _, want := range []string{
		"会员归属", "1024", "白银会员", "手工指定",

		"2026-09-26 10:00",
		"解除锁定",

		"自动升级尚未启用",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}

	if strings.Contains(body, `>manual<`) {
		t.Errorf("来源列渲染的是原始枚举值，说明少了按枚举值取词那一层")
	}
}

func TestMembershipAssignmentsEmptyRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership_assignments.html", assignmentsPageFixture(map[string]any{
		"Rows": []membershipAssignRow{}, "Total": int64(0),
	}))
	if !strings.Contains(body, "还没有会员归属") {
		t.Errorf("空态文案缺失")
	}
}

func TestMembershipAssignSetSelectHasNoAllOption(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership_assignments.html", assignmentsPageFixture(nil))
	start := strings.Index(body, `id="ma-tier"`)
	if start < 0 {
		t.Fatalf("手工指定表单的等级下拉不见了")
	}
	end := strings.Index(body[start:], "</select>")
	if end < 0 {
		t.Fatalf("等级下拉没有收尾标签")
	}
	seg := body[start : start+end]
	if strings.Contains(seg, `value=""`) || strings.Contains(seg, `value="0"`) {
		t.Errorf("手工指定表单不该有「全部」这类空值 / 0 值选项：%s", seg)
	}
	if !strings.Contains(seg, `value="2"`) {
		t.Errorf("真实等级没进选项：%s", seg)
	}
}

func TestMembershipTierRowOfKeepsFacts(t *testing.T) {
	row := membershipTierRowOf(&membershipdto.TierResp{
		ID: 7, Name: "黄金会员", ThresholdAmount: 100005,
		Entitlements: []membershipdto.EntitlementResp{
			{Kind: membershipenums.KindDiscount, ValueInt: 20},
			{Kind: membershipenums.KindFreeShipping, ValueInt: 1},
		},
	})
	if row.ThresholdAmount != 100005 {
		t.Errorf("门槛应保持分（100005），实际 %d", row.ThresholdAmount)
	}
	if !row.FreeShipping || !row.HasDiscount || row.DiscountPercent != 20 {
		t.Errorf("权益视图 = %+v", row)
	}
	if !row.HasEntitlement {
		t.Errorf("有权益时 HasEntitlement 应为真")
	}
}

func TestMembershipTierRowOfNoEntitlement(t *testing.T) {
	row := membershipTierRowOf(&membershipdto.TierResp{ID: 1, Name: "普通会员"})
	if row.HasEntitlement || row.FreeShipping || row.HasDiscount || row.DiscountPercent != 0 {
		t.Errorf("无权益视图 = %+v", row)
	}
}

func TestMembershipSourceOptionsCarryAllValue(t *testing.T) {
	opts := membershipSourceOptions(membershipmodel.SourceManual)
	if len(opts) != 3 || opts[0].Value != "" {
		t.Fatalf("来源下拉 = %+v", opts)
	}
	if opts[0].Selected != false || opts[2].Selected != true {
		t.Fatalf("选中态应由服务端算好：%+v", opts)
	}

	typ := reflect.TypeOf(opts[0])
	if _, has := typ.FieldByName("Label"); has {
		t.Fatalf("来源选项类型上出现了 Label 字段（文案应归模板）：%+v", typ)
	}
}

func TestMembershipTierOptionsHaveNoAllOption(t *testing.T) {
	tiers := []*membershipdto.TierResp{{ID: 1, Name: "普通会员"}, {ID: 2, Name: "白银会员"}}
	opts := membershipTierOptions(tiers, 2)
	if len(opts) != 2 {
		t.Fatalf("等级选项应只有真实等级，实际 %+v", opts)
	}
	for _, opt := range opts {
		if opt.Value == int64(0) {
			t.Errorf("不该出现 0 值选项：%+v", opt)
		}
	}
	if opts[1].Selected != true || opts[0].Selected != false {
		t.Errorf("选中态应由服务端算好：%+v", opts)
	}
}

type stubMembershipService struct {
	membershipcontract.MembershipService
}

func TestMembershipPageIgnoresForgedErrQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/membership", pageview.Slot(MembershipTiersPageTemplate, adminui.RenderPage), NewMembershipPageHandle(&stubMembershipService{}, nil).MembershipPage)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/membership?err=%E4%BC%AA%E9%80%A0%E6%96%87%E6%A1%88", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("页面应正常渲染，实际 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "伪造文案") {
		t.Fatalf("伪造的 ?err= 不应进入页面")
	}
}

func TestMembershipErrStatusMapsBusinessKeys(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{membershipenums.ErrDetail(membershipenums.ErrNotFound, "tier_id=3"), http.StatusNotFound},
		{membershipenums.ErrDetail(membershipenums.ErrTierNameTaken, "白银会员"), http.StatusConflict},
		{membershipenums.Err(membershipenums.ErrDefaultTierRequired), http.StatusConflict},
		{membershipenums.ErrDetail(membershipenums.ErrDefaultTierMissing, "p-1"), http.StatusBadRequest},
		{membershipenums.Err(membershipenums.ErrRecalcUnavailable), http.StatusServiceUnavailable},
		{membershipenums.Err(membershipenums.ErrInvalidParam), http.StatusBadRequest},
	}
	for _, tc := range cases {
		fact, ok := facing.FactOf(tc.err)
		if !ok || !fact.Business {
			t.Fatalf("%v 未被判成业务错误", tc.err)
		}
		if got := membershipErrStatus(fact); got != tc.want {
			t.Errorf("%v → %d，期望 %d", tc.err, got, tc.want)
		}
	}
}
