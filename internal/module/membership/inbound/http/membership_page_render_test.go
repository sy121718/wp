package membershiphttp

// membership_page_render_test.go — 两个后台页的渲染冒烟（BIZ-3）。
//
// 钉住三件事：
//   - 模板的 Jet 语法与 layout 数据契约成立（模板写错只会在用户点开时 500，
//     而 500 的表现是通用错误页 —— 不是「少一块内容」那么容易看出来）；
//   - 服务端给的值真的渲染到页面上（不是「接口对了、页面空白」）；
//   - 空态与降级分支也渲染完整页面（缺 key 会 renderError → 整个响应被丢弃）。
//
// 用真实 Jet 渲染器与真实模板文件（不是含内联模板的假 engine）。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipmodel "go_wp/internal/module/membership/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"
)

// membershipRenderData 渲染两个页面所需的公共外壳键（与 shell.Prepare 注入的键同集）。
//
// 手工给而不是调 shell.Prepare：Prepare 依赖 admin 权限契约与 i18n 组件，
// 而本测试要钉的是**模板的数据契约**，不是装配链路（那由 feature 测试覆盖）。
func membershipRenderData(lang string) gin.H {
	return gin.H{
		"lang": lang, "t": templates.TranslateFunc(lang),
		"langs": templates.LanguageOptions(lang), "lang_redirect": "/admin/membership",
		"csrf_token": "test-token",
		"PermSet": map[string]bool{
			"membership:tier_create": true, "membership:tier_update": true,
			"membership:tier_delete": true, "membership:assign_set": true,
			"membership:assign_unlock": true,
		},
		"NavGroups": []any{}, "SidebarOpen": false, "SidebarPinned": false, "HasSubnav": false,
	}
}

// renderMembershipPage 用真实渲染器渲染一个后台页并返回响应体（非 200 直接失败）。
func renderMembershipPage(t *testing.T, tpl string, over map[string]any) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	data := membershipRenderData("zh-CN")
	for k, v := range over {
		data[k] = v
	}
	engine := gin.New()
	// 模板根相对包目录：internal/module/membership/inbound/http → internal/templates。
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
	// 整页渲染的判据：响应里必须有完整的收尾标签。
	// 半截 HTML 是本仓渲染器「先渲到 buffer、失败就丢弃」之前的历史故障形态，
	// 这条断言让那种情况不可能悄悄回来。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("响应不是完整页面（缺 </html>），长度 %d", len(body))
	}
	return body
}

// tiersPageFixture 等级页的完整数据（照 handler 的 templateMap 键集给齐）。
func tiersPageFixture(over map[string]any) map[string]any {
	data := map[string]any{
		"title": "admin.membership.title", "menu": "membership",
		"Projects":        []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}},
		"SelectedProject": "p-1",
		"Tiers": []membershipTierRow{
			{ID: 1, Name: "普通会员", SortOrder: 0, ThresholdYuan: "0.00", IsDefault: true},
			{ID: 2, Name: "白银会员", SortOrder: 1, ThresholdYuan: "1000.00",
				FreeShipping: true, DiscountText: "5%", DiscountRaw: "5", HasEntitlement: true},
		},
		"HasTiers":              true,
		"KindFreeShippingValue": "free_shipping", "KindDiscountValue": "discount",
		"KindFreeShippingLabel": "免运费", "KindDiscountLabel": "折扣",
		"Err": "", "Done": "", "LoadFailed": false,
	}
	for k, v := range over {
		data[k] = v
	}
	return data
}

// TestMembershipTiersPageRenders 等级页把等级、门槛（元）、默认徽标与权益都渲染出来。
func TestMembershipTiersPageRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(nil))
	for _, want := range []string{
		"会员等级与权益", "普通会员", "白银会员",
		// 门槛按「元」渲染（库里是分，换算只发生在 handler 边界）。
		"1000.00",
		// 默认徽标只出现在默认等级那一行。
		"默认等级",
		// 权益：免运费 + 折扣百分比。
		"免运费", "5%",
		// 新建入口（PermSet 里有 tier_create）。
		"tpl-tier-create",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}
}

// TestMembershipTiersPageEditRegionRenders 带 ?edit= 时编辑区出现且字段回填。
func TestMembershipTiersPageEditRegionRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Edit": &membershipTierEdit{
			ID: 2, Name: "白银会员", SortOrder: 1, ThresholdYuan: "1000.00",
			Remark: "满千升级", FreeShipping: true, DiscountPercent: "5",
		},
	}))
	for _, want := range []string{
		"编辑等级", "白银会员", "满千升级",
		// 权益表单的两个取值都回填（折扣 input 的 value）。
		`name="discountPercent" value="5"`,
		// 非默认等级才有「设为默认等级」按钮。
		"设为默认等级",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑区缺少 %q", want)
		}
	}
}

// TestMembershipTiersPageEmptyAndLoadFailed 空态与降级分支都必须是完整页面。
//
// 判据来自 internal/templates/CLAUDE.md：错误分支少给一个必需键，
// 后果不是「少渲染一块」而是整页渲染中断（响应被丢弃）。
func TestMembershipTiersPageEmptyAndLoadFailed(t *testing.T) {
	empty := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Tiers": []membershipTierRow{}, "HasTiers": false,
	}))
	if !strings.Contains(empty, "这个工程还没有等级") {
		t.Errorf("空态文案缺失")
	}
	// 空态下不能出现「设为默认」这类只属于某一行编辑区的动作。
	if strings.Contains(empty, "危险操作") {
		t.Errorf("没有 ?edit= 时不该渲染编辑区")
	}

	fallback := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Projects": []projectcontract.ProjectResp{}, "SelectedProject": "",
		"Tiers": []membershipTierRow{}, "HasTiers": false,
		"LoadFailed": true, "Err": "工程列表没读出来",
	}))
	if !strings.Contains(fallback, "工程列表没读出来") {
		t.Errorf("降级分支的提示缺失")
	}
}

// assignmentsPageFixture 归属页的完整数据。
func assignmentsPageFixture(over map[string]any) map[string]any {
	data := map[string]any{
		"title": "admin.membership.assign.title", "menu": "membership-assignments",
		"Projects":        []projectcontract.ProjectResp{{ID: "p-1", Name: "演示站"}},
		"SelectedProject": "p-1",
		"TierOptions": []gin.H{
			{"Value": int64(0), "Label": "全部", "Selected": false},
			{"Value": int64(1), "Label": "普通会员", "Selected": false},
			{"Value": int64(2), "Label": "白银会员", "Selected": true},
		},
		"SourceOptions": []gin.H{
			{"Value": "", "Label": "全部", "Selected": true},
			{"Value": "auto", "Label": "自动重算", "Selected": false},
			{"Value": "manual", "Label": "手工指定", "Selected": false},
		},
		"FilterTier": int64(0), "FilterSource": "", "FilterUser": uint64(0),
		"Rows": []membershipAssignRow{
			{ID: 9, UserID: 1024, TierID: 2, TierName: "白银会员", Source: "manual",
				SourceText: "手工指定", IsManual: true, AssignedAt: "2026-09-26 10:00:00"},
		},
		"Total": int64(1),
		"Err":   "", "Done": "", "LoadFailed": false, "RecalcAvailable": false,
		"PaginationInfo": "", "PaginationLinks": nil,
	}
	for k, v := range over {
		data[k] = v
	}
	return data
}

// TestMembershipAssignmentsPageRenders 归属页把行数据、来源与解锁入口都渲染出来。
func TestMembershipAssignmentsPageRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership_assignments.html", assignmentsPageFixture(nil))
	for _, want := range []string{
		"会员归属", "1024", "白银会员", "手工指定", "2026-09-26 10:00:00",
		"解除锁定",
		// 端口未接入时的显式说明（而不是把按钮藏起来让人以为功能不存在）。
		"自动升级尚未启用",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}
}

// TestMembershipAssignmentsEmptyRenders 空态是完整页面且给出下一步指引。
func TestMembershipAssignmentsEmptyRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership_assignments.html", assignmentsPageFixture(map[string]any{
		"Rows": []membershipAssignRow{}, "Total": int64(0),
	}))
	if !strings.Contains(body, "还没有会员归属") {
		t.Errorf("空态文案缺失")
	}
}

// TestMembershipTierRowOfFormatsThreshold 行视图做的是「分 → 元」的唯一一次换算。
func TestMembershipTierRowOfFormatsThreshold(t *testing.T) {
	row := membershipTierRowOf(&membershipdto.TierResp{
		ID: 7, Name: "黄金会员", ThresholdAmount: 100005,
		Entitlements: []membershipdto.EntitlementResp{
			{Kind: "discount", ValueInt: 20},
			{Kind: "free_shipping", ValueInt: 1},
		},
	})
	if row.ThresholdYuan != "1000.05" {
		t.Errorf("门槛换算 = %q，期望 1000.05", row.ThresholdYuan)
	}
	if !row.FreeShipping || row.DiscountRaw != "20" || row.DiscountText != "20%" {
		t.Errorf("权益视图 = %+v", row)
	}
	if !row.HasEntitlement {
		t.Errorf("有权益时 HasEntitlement 应为真")
	}
}

// TestMembershipTierRowOfNoEntitlement 没有任何权益时 HasEntitlement 为假（页面据此显示「未配置权益」）。
func TestMembershipTierRowOfNoEntitlement(t *testing.T) {
	row := membershipTierRowOf(&membershipdto.TierResp{ID: 1, Name: "普通会员"})
	if row.HasEntitlement || row.FreeShipping || row.DiscountRaw != "" {
		t.Errorf("无权益视图 = %+v", row)
	}
}

// TestMembershipSourceOptionsCarryAllValue 来源下拉必须有「全部」这一项（空值）。
//
// 少了它，筛选栏无法表达「不筛来源」—— 页面看起来正常，只是永远被钉在第一个选项上。
func TestMembershipSourceOptionsCarryAllValue(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/membership/assignments", nil)
	opts := membershipSourceOptions(c, membershipmodel.SourceManual)
	if len(opts) != 3 || opts[0]["Value"] != "" {
		t.Fatalf("来源下拉 = %+v", opts)
	}
	if opts[0]["Selected"] != false || opts[2]["Selected"] != true {
		t.Fatalf("选中态应由服务端算好：%+v", opts)
	}
}

// TestMembershipPageErrRejectsForgedNotice 查询参数不是可信边界：伪造的 ?err= 必须被丢掉。
func TestMembershipPageErrRejectsForgedNotice(t *testing.T) {
	c, rec := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/membership?err=%E4%BC%AA%E9%80%A0%E6%96%87%E6%A1%88", nil)
	got := membershipPageErr(c)
	_ = rec
	if strings.Contains(got, "伪造文案") {
		t.Errorf("伪造文案被原样透出：%q", got)
	}
}

// TestMembershipErrStatusMapsBusinessKeys 状态码映射按业务 key（含带定位信息的形态）。
func TestMembershipErrStatusMapsBusinessKeys(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{errString("membership.err.notFound：tier_id=3"), http.StatusNotFound},
		{errString("membership.err.tierNameTaken：白银会员"), http.StatusConflict},
		{errString("membership.err.defaultTierRequired"), http.StatusConflict},
		{errString("membership.err.defaultTierMissing：p-1"), http.StatusBadRequest},
		{errString("membership.err.recalcUnavailable"), http.StatusServiceUnavailable},
		{errString("pq: relation \"membership_tiers\" does not exist"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		if got := membershipErrStatus(tc.err); got != tc.want {
			t.Errorf("%v → %d，期望 %d", tc.err, got, tc.want)
		}
	}
}

// errString 最小 error 实现（测试里造带定位信息的业务错误）。
type errString string

func (e errString) Error() string { return string(e) }

// 断言 shell 包被本文件使用（翻译函数在测试数据里注入）。
var _ = shell.TranslateFor
