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

// membershipRenderData 渲染两个页面所需的公共外壳键（与 adminui.Prepare 注入的键同集）。
//
// 手工给而不是调 adminui.Prepare：Prepare 依赖 admin 权限契约与 i18n 组件，
// 而本测试要钉的是**模板的数据契约**，不是装配链路（那由 feature 测试覆盖）。
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
//
// 行数据给的是**业务事实**（门槛的分、折扣的原始数字），不是展示串 ——
// 模板负责 money / percent / tr，这正是本批要钉住的边界。
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

// TestMembershipTiersPageRenders 等级页把等级、门槛（元）、默认徽标与权益都渲染出来。
func TestMembershipTiersPageRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(nil))
	for _, want := range []string{
		"会员等级与权益", "普通会员", "白银会员",
		// 门槛按「元」渲染 —— 库里是分，换算发生在**模板**（money(100000, "") → "1000.00"）。
		"1000.00",
		// 默认徽标只出现在默认等级那一行（非默认那行是「—」，见空值口径）。
		"默认等级",
		// 权益：免运费 + 折扣百分比（percent(5, 0) → "5%"）。
		"免运费", "5%",
		// 新建入口（PermSet 里有 tier_create）。
		"tpl-tier-create",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}
}

// TestMembershipTiersPageConvertsCentsInTemplate 非整元门槛的换算由模板完成，且不丢精度。
//
// 100005 分 = 1000.05 元：handler 既不该预先拼成 "1000.05"，也不能把它当整数元（那样是 1000.05 元 → 1000 元）。
// 这条同时是「行视图里没有格式化」的守卫 —— 换回 Go 侧格式化时，前缀串会立刻不一样。
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

// TestMembershipTiersPageEditRegionRenders 带 ?edit= 时编辑区出现且字段回填。
func TestMembershipTiersPageEditRegionRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Edit": &membershipTierEdit{
			ID: 2, Name: "白银会员", SortOrder: 1, ThresholdAmount: 100000,
			Remark: "满千升级", FreeShipping: true, HasDiscount: true, DiscountPercent: 5,
		},
	}))
	for _, want := range []string{
		"编辑等级", "白银会员", "满千升级",
		// 门槛回填走同一条换算（分 → 元），不能出现原始分数。
		`name="thresholdYuan" value="1000.00"`,
		// 有折扣权益时折扣 input 回填原始数字。
		`name="discountPercent" value="5"`,
		// 非默认等级才有「设为默认等级」按钮。
		"设为默认等级",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑区缺少 %q", want)
		}
	}
}

// TestMembershipTiersPageEditLeavesDiscountEmptyWithoutEntitlement 没有折扣权益时折扣 input 留空。
//
// 回填一个 0 的话，运营点保存就会被取值域（1..100）打回 —— 而表单看起来「本来就是 0」。
func TestMembershipTiersPageEditLeavesDiscountEmptyWithoutEntitlement(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership.html", tiersPageFixture(map[string]any{
		"Edit": &membershipTierEdit{ID: 1, Name: "普通会员", ThresholdAmount: 0, IsDefault: true},
	}))
	if !strings.Contains(body, `name="discountPercent" value=""`) {
		t.Errorf("没有折扣权益时应留空回填（不能是 0）")
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
		"LoadFailed": true, "LoadErr": "工程列表没读出来",
	}))
	if !strings.Contains(fallback, "工程列表没读出来") {
		t.Errorf("降级分支的提示缺失")
	}
}

// assignmentsPageFixture 归属页的完整数据。
//
// 键集与形态都照 handler 的 templateMap：
//   - 下拉只给**取值 + 选中态**（选项文案模板自己 tr 取词，Go 侧不产出界面文案）；
//   - TierOptions 里**没有**「全部」（筛选栏的那一项由模板自己写，手工指定表单不能有它）；
//   - 行给原始枚举值与原始时刻。
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
		// 分页事实：零值＝没有分页（本夹具的页只有 1 条）。
		"Pagination": pagination.Page{},
		// 来源的两个取值（模板按它们判这一行显示什么、能不能解除锁定）。
		"SourceManual": membershipmodel.SourceManual,
		"SourceAuto":   membershipmodel.SourceAuto,
	}
	for k, v := range over {
		data[k] = v
	}
	return data
}

// membershipAssignTestTime 固定时刻（用本地时区构造：dateTime 按本地时区渲染，
// 用 UTC 会让断言随跑测试的机器时区变）。
func membershipAssignTestTime() utils.JSONTime {
	return utils.JSONTime(time.Date(2026, 9, 26, 10, 0, 0, 0, time.Local))
}

// TestMembershipAssignmentsPageRenders 归属页把行数据、来源与解锁入口都渲染出来。
func TestMembershipAssignmentsPageRenders(t *testing.T) {
	body := renderMembershipPage(t, "admin/membership/membership_assignments.html", assignmentsPageFixture(nil))
	for _, want := range []string{
		"会员归属", "1024", "白银会员", "手工指定",
		// 时刻由模板按本地时区格式化（到分，与订单 / 区块页同精度）。
		"2026-09-26 10:00",
		"解除锁定",
		// 端口未接入时的显式说明（而不是把按钮藏起来让人以为功能不存在）。
		"自动升级尚未启用",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("渲染结果缺少 %q", want)
		}
	}
	// 原始枚举值不该漏到页面上（页面上是显示名）。
	if strings.Contains(body, `>manual<`) {
		t.Errorf("来源列渲染的是原始枚举值，说明少了按枚举值取词那一层")
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

// TestMembershipAssignSetSelectHasNoAllOption 手工指定表单的等级下拉不能有「全部」。
//
// handler 不再往 TierOptions 里塞「全部」那一项（它只属于筛选栏）：留着的话
// 运营点中它提交 tierId=0，service 只能回一句「等级不存在」—— 而界面看起来是个正常选项。
// 这里直接数渲染结果里那段 HTML，防的是「模板又在手工指定表单里补了一个空值选项」。
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

// TestMembershipTierRowOfKeepsFacts 行视图只搬运事实：门槛留「分」、折扣留原始数字。
//
// 换算与文案都在模板里（money / percent）—— 行视图一旦自己格式化，
// 「同一份加工散在两处」的漂移就回来了，而且行视图会重新依赖取词（不可单测）。
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

// TestMembershipTierRowOfNoEntitlement 没有任何权益时 HasEntitlement 为假（页面据此显示「未配置权益」）。
func TestMembershipTierRowOfNoEntitlement(t *testing.T) {
	row := membershipTierRowOf(&membershipdto.TierResp{ID: 1, Name: "普通会员"})
	if row.HasEntitlement || row.FreeShipping || row.HasDiscount || row.DiscountPercent != 0 {
		t.Errorf("无权益视图 = %+v", row)
	}
}

// TestMembershipSourceOptionsCarryAllValue 来源下拉必须有「全部」这一项（空值）。
//
// 少了它，筛选栏无法表达「不筛来源」—— 页面看起来正常，只是永远被钉在第一个选项上。
func TestMembershipSourceOptionsCarryAllValue(t *testing.T) {
	opts := membershipSourceOptions(membershipmodel.SourceManual)
	if len(opts) != 3 || opts[0].Value != "" {
		t.Fatalf("来源下拉 = %+v", opts)
	}
	if opts[0].Selected != false || opts[2].Selected != true {
		t.Fatalf("选中态应由服务端算好：%+v", opts)
	}
	// 选项**不带文案**：显示名是 UI 文案，模板按取值 tr() 取词（与来源列同一个 key）。
	//
	// 强类型之后这一点由**类型**保证：ValueOption 根本没有 Label 字段。
	// 下面这条断言退化成「类型上确实没有」，而不是运行时查 map。
	typ := reflect.TypeOf(opts[0])
	if _, has := typ.FieldByName("Label"); has {
		t.Fatalf("来源选项类型上出现了 Label 字段（文案应归模板）：%+v", typ)
	}
}

// TestMembershipTierOptionsHaveNoAllOption 等级筛选下拉的取值集合里没有「全部」（0）。
//
// handler 侧不能再造出 tierId=0 —— 筛选栏的「全部」是模板里那个 `value=""` 的选项。
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

// stubMembershipService 的 nil 契约令意外 service 调用直接暴露。
type stubMembershipService struct {
	membershipcontract.MembershipService
}

// TestMembershipPageIgnoresForgedErrQuery 查询参数不是可信边界：伪造的 ?err= 不再有任何读侧通道，
// 页面必须把它当普通 query 丢掉（原读侧判定已整批删除，这条覆盖改指新目标）。
func TestMembershipPageIgnoresForgedErrQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/membership", adminui.PageSlot(MembershipTiersPageTemplate), NewMembershipPageHandle(&stubMembershipService{}, nil).MembershipPage)

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

// TestMembershipErrStatusMapsBusinessKeys 状态码映射按业务 key（判据是 pkg/facing.FactOf 给出的 Key）。
//
// 内部错误不在这张表里：membershipFail 对它一律走 response.ErrorInternal（500 + 通用文案），
// 覆盖在 membership_facing_text_test.go 的 TestMembershipJSONInternalErrorUsesFacingKey。
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
