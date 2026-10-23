package userhttp

// customer_view_facts_test.go — 「展示加工外移」的分工契约测试。
//
// 守的是**分工**，不是某一段 HTML：Go 侧的行投影只给事实（状态值、时刻本身、各个原值字段），
// 色调 / 展示名回退 / 时间格式化 / 详情链接都属于展示，落在模板里（Jet + ui.css 的
// .badge-<tone> 语义档别名）。
//
// 为什么需要单独一组用例：展示字段从 Go 移走之后，页面长得一模一样 —— 既有断言
// 一条都不会变红。于是「谁负责展示」这件事没有任何东西守着：下次谁在 customerRow 里
// 顺手加回一个 StatusTone，或者模板回头去读 dto 上预制的展示串（*Text / *Label），
// 都要先踩红这几条。
//
// 反面参照（本仓真实反例）：order 的 couponStatusBadge(statusLabel string) —— 拿已翻译的
// 文案反查样式表，改一句词条就让样式静默失效，不报错、测试也不红。

import (
	"go_wp/internal/adminui"
	"go_wp/pkg/utils"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	userdto "go_wp/internal/module/user/dto"
)

// —— 一、Go 侧的行投影：事实进、展示不进 ——

// TestCustomerRowCarriesFactsNotPresentation 行投影不许再预制展示字段。
func TestCustomerRowCarriesFactsNotPresentation(t *testing.T) {
	row := customerRow(customerSample())
	// 强类型之后"字段在不在"由编译期保证；这里守住的是**不该有的字段没有**与**该有的还在**。
	typ := reflect.TypeOf(row)

	for _, banned := range []string{
		"DisplayLabel",      // 展示名回退（展示名 → 昵称 → 登录名）归模板
		"StatusTone",        // 状态色调归模板（按状态值取语义档）
		"EmailVerifiedTone", // 邮箱验证色调同理
		"DetailURL",         // 详情链接归模板（按 ID 拼）
		"RegisteredAtText",  // 时间格式化归模板的 dateTime()
		"LastLoginTimeText", //
		"LockedUntilText",   //
	} {
		if _, ok := typ.FieldByName(banned); ok {
			t.Errorf("customerRow 又预制了展示字段 %q：展示判断归模板（本批口径）", banned)
		}
	}
	for _, want := range []string{
		"DisplayName", "Nickname", "Username", "Status", "EmailVerified",
		"RegisteredAt", "LastLoginTime", "LockedUntil",
		// 文案链仍是「枚举 key + 中文兜底」（userenums 的形态），模板取词。
		"StatusLabelKey", "StatusLabelFallback", "EmailVerifiedKey", "EmailVerifiedFallback",
		// 动作权限事实（服务端判定，不由模板推）。
		"Actionable", "NextStatus", "StatusActionKey", "StatusActionLabel",
	} {
		if _, ok := typ.FieldByName(want); !ok {
			t.Errorf("customerRow 缺少事实字段 %q", want)
		}
	}
	// 状态说明是**按状态出现**的键（键存在 = 这个状态有说明要讲），单独钉两档。
	pending := customerSample()
	pending.Status = customerStatusPending
	if customerRow(pending).PendingHintKey == "" {
		t.Error("待激活状态的行应带 PendingHintKey（详情页的说明靠它取词）")
	}
	locked := customerSample()
	locked.Locked = true
	if customerRow(locked).LockHintKey == "" {
		t.Error("锁定状态的行应带 LockHintKey（不能只画徽章、不给解除锁定的说明）")
	}
	// 时间给的是**时刻本身**：模板的 dateTime() 认 *utils.JSONTime（nil / 零值出空串）。
	if _, ok := row.RegisteredAt.(*utils.JSONTime); !ok {
		t.Errorf("RegisteredAt 应是 *utils.JSONTime（交给模板格式化），实得 %T", row.RegisteredAt)
	}
}

// TestCustomerCounterTabsCarryNoTone 计数徽章只给键 / 计数 / URL，色调由模板按键取。
func TestCustomerCounterTabsCarryNoTone(t *testing.T) {
	tabs := customerCounterTabs(userdto.CustomerCounters{Total: 3, Active: 1, Disabled: 1, Pending: 1},
		customerFilter{Status: customerStatusAll, EmailVerified: userdto.EmailVerifiedAll})
	if len(tabs) != 7 {
		t.Fatalf("计数徽章应有 7 个，实得 %d", len(tabs))
	}
	// 强类型之后「字段在不在」由编译期保证；这里守的是**色调不由 Go 给** ——
	// 判据是类型上有没有 Badge/Tone 这类字段（有就是又加回来了）。
	typ := reflect.TypeOf(tabs[0])
	for _, banned := range []string{"Badge", "Tone", "Class", "StatusClass"} {
		if _, ok := typ.FieldByName(banned); ok {
			t.Errorf("计数徽章的色调不该由 Go 给：类型上出现了字段 %q", banned)
		}
	}
}

// TestCustomerRfmSegmentTabsCarryNoTone 分段徽章同理；URL 仍归服务端（带时间窗口与其它条件）。
func TestCustomerRfmSegmentTabsCarryNoTone(t *testing.T) {
	tabs := customerRfmSegmentTabs(rfmSample(), "", customerOverviewRange{From: "2026-10-01", To: "2026-10-05"})
	if len(tabs) != 4 {
		t.Fatalf("分段徽章应有 4 个（全部 / 高价值 / 潜力 / 一般），实得 %d", len(tabs))
	}
	typ := reflect.TypeOf(tabs[0])
	for _, banned := range []string{"Badge", "Tone", "Class"} {
		if _, ok := typ.FieldByName(banned); ok {
			t.Errorf("分段徽章的色调不该由 Go 给：类型上出现了字段 %q", banned)
		}
	}
	for _, tab := range tabs {
		if !strings.Contains(tab.URL, "from=2026-10-01") {
			t.Errorf("分段徽章 %v 的 URL 丢了时间窗口：%v", tab.Key, tab.URL)
		}
	}
}

// —— 二、色调由模板按事实取（Go 只给状态值）——

// TestCustomerListStatusToneComesFromTemplate 列表行的状态徽章按状态值取语义档。
//
// 未知取值必须落 mute：把一个不认识的状态渲染成「成功」或「失败」，会让运营
// 对一个自己看不懂的状态产生确定的误会。
func TestCustomerListStatusToneComesFromTemplate(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   string
		label  string // 空串表示不断言标签（未知状态的兜底文案由 enums 决定）
	}{
		{"正常", 1, "badge-ok", "正常"},
		{"已停用", 0, "badge-danger", "已停用"},
		{"待激活", 2, "badge-warn", "待激活"},
		{"未知状态", 7, "badge-mute", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := customerSample()
			item.Status = tc.status
			body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
				customerListPageData(customerTestPageTitle, &userdto.CustomerListResp{
					List: []*userdto.CustomerResp{item}, Total: 1,
				}, customerFilter{Status: customerStatusAll}, 1, 20, customerMsg{}, false)))

			want := `<span class="badge ` + tc.want + `">` + tc.label
			if !strings.Contains(body, want) {
				t.Errorf("状态 %d 的状态徽章应形如 %q，渲染结果里没有它", tc.status, want)
			}
		})
	}
}

// TestCustomerCounterToneComesFromTemplate 计数徽章的色调按口径键取（七档一一对应）。
func TestCustomerCounterToneComesFromTemplate(t *testing.T) {
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(customerTestPageTitle, customerListSample(), customerFilter{Status: customerStatusAll}, 1, 20, customerMsg{}, false)))

	cases := []struct{ url, tone string }{
		{"/admin/customers", "mute"},                 // 全部
		{"/admin/customers?status=1", "ok"},          // 正常
		{"/admin/customers?status=0", "danger"},      // 已停用
		{"/admin/customers?status=2", "warn"},        // 待激活
		{"/admin/customers?locked=1", "info"},        // 已锁定
		{"/admin/customers?emailVerified=1", "mute"}, // 已验证
		{"/admin/customers?emailVerified=2", "mute"}, // 未验证
	}
	for _, tc := range cases {
		want := `<a class="badge badge-` + tc.tone + `" href="` + tc.url + `"`
		if !strings.Contains(body, want) {
			t.Errorf("计数徽章 %s 的色调应为 %s，渲染结果里没有 %q", tc.url, tc.tone, want)
		}
	}
}

// TestCustomerDetailToneComesFromTemplate 详情页的状态 / 邮箱验证徽章档位同样由模板取。
//
// 档位映射与列表页共用 customer_badges.html 的片段，所以这条与
// TestCustomerListStatusToneComesFromTemplate 一起钉住「同一个状态在两页是同一档」——
// 两处各写一份时，改一边忘一边的症状是列表页绿、详情页灰，不报错也没有别的测试会红。
func TestCustomerDetailToneComesFromTemplate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"正常", 1, `badge-ok">正常`},
		{"已停用", 0, `badge-danger">已停用`},
		{"待激活", 2, `badge-warn">待激活`},
		// 未知取值落 mute，但标签仍显示服务端给的兜底文案（不渲染成一个空徽章）。
		{"未知状态落 mute 并显示兜底", 7, `badge-mute">未知状态(7)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := customerSample()
			item.Status = tc.status
			body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(
				customerDetailPageData(customerTestDetailTitle, nil, item, detailProjects(), "p1", detailSummary(), false, false, customerMsg{})))

			if want := `<span class="badge ` + tc.want + `</span></h2>`; !strings.Contains(body, want) {
				t.Errorf("详情页状态徽章应为 %q", want)
			}
		})
	}

	for _, tc := range []struct {
		name     string
		verified bool
		want     string
	}{
		{"已验证", true, `badge-ok">邮箱已验证`},
		{"未验证", false, `badge-warn">邮箱未验证`},
	} {
		t.Run("邮箱"+tc.name, func(t *testing.T) {
			item := customerSample()
			item.EmailVerified = tc.verified
			body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(
				customerDetailPageData(customerTestDetailTitle, nil, item, detailProjects(), "p1", detailSummary(), false, false, customerMsg{})))

			if want := `<span class="badge ` + tc.want + `</span>`; !strings.Contains(body, want) {
				t.Errorf("详情页邮箱验证徽章应为 %q", want)
			}
		})
	}
}

// —— 三、展示名回退在模板里（列表与详情各一份实现，两边都要钉）——

// TestCustomerDisplayNameFallbackIsRenderedByTemplate 三级回退在模板里（列表与详情各一份实现，两边都要钉）。
//
// 判空必须用 trimSpace：库里的脏值可能是 "   "（空白串）。移走 Go 侧 customerDisplayLabel 时
// 这里曾退化成 `== ""`，空白串就挡住了整条回退链 —— 页面显示一列空白，看不出这个人是谁。
// 旧实现是 strings.TrimSpace 判定、被挑中的值原样输出（不修剪首尾），下面的用例把两点都钉住。
func TestCustomerDisplayNameFallbackIsRenderedByTemplate(t *testing.T) {
	cases := []struct {
		name, display, nickname, username, want string
	}{
		{"展示名优先", "艾丽丝", "小艾", "alice", "艾丽丝"},
		{"回退昵称", "", "小艾", "alice", "小艾"},
		{"回退登录名", "", "", "alice", "alice"},
		{"都空落占位", "", "", "", "—"},
		{"展示名是空白串则回退昵称", "   ", "小艾", "alice", "小艾"},
		{"昵称是空白串则回退登录名", "", " \t ", "alice", "alice"},
		{"展示名与昵称都是空白串则回退登录名", "  ", "\t ", "alice", "alice"},
		{"三个都是空白串落占位", "  ", "\t", " \t ", "—"},
		{"展示名带首尾空白按原值显示", "  艾丽丝 ", "", "", "  艾丽丝 "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := customerSample()
			item.DisplayName, item.Nickname, item.Username = tc.display, tc.nickname, tc.username

			listBody := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
				customerListPageData(customerTestPageTitle, &userdto.CustomerListResp{
					List: []*userdto.CustomerResp{item}, Total: 1,
				}, customerFilter{Status: customerStatusAll}, 1, 20, customerMsg{}, false)))
			if want := `class="row-title-link" href="/admin/customers/detail?id=42">` + tc.want + `</a>`; !strings.Contains(listBody, want) {
				t.Errorf("列表页的展示名应为 %q（三级回退在模板里）", tc.want)
			}

			detailBody := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(
				customerDetailPageData(customerTestDetailTitle, nil, item, nil, "", nil, false, false, customerMsg{})))
			if want := `<h2>` + tc.want + ` <span class="badge`; !strings.Contains(detailBody, want) {
				t.Errorf("详情页的展示名应为 %q（三级回退在模板里）", tc.want)
			}
		})
	}
}

// —— 四、时刻缺失与认不出的枚举值：模板必须给出可读的兜底，而不是空白 ——

// TestCustomerDetailEmptyTimesRenderPlaceholder 时刻为空时落「—」而不是空白单元格。
func TestCustomerDetailEmptyTimesRenderPlaceholder(t *testing.T) {
	item := customerSample()
	item.RegisteredAt = nil
	item.LastLoginTime = nil
	body := renderCustomerAdminTemplate(t, "admin/user/customer_detail.html", customerTestLayoutData(
		customerDetailPageData(customerTestDetailTitle, nil, item, nil, "", nil, false, false, customerMsg{})))

	for _, want := range []string{
		"<dt>注册时间</dt><dd>—</dd>",
		"<dt>最近登录</dt><dd>—</dd>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少 %q（时刻为空要显式占位，不能渲染成空白）", want)
		}
	}
}

// TestCustomerRfmUnknownSegmentRendersRawKey 认不出的分段原样显示。
//
// 口径变了而这个映射没跟上时，页面上出现一个陌生的英文词比出现一个空单元格
// 更容易被发现（这是移走 customerRfmSegmentText 时被保留下来的行为）。
func TestCustomerRfmUnknownSegmentRendersRawKey(t *testing.T) {
	res := rfmSample()
	res.Items = res.Items[:1]
	res.Items[0].Segment = "lapsed"

	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil,
		fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(&fakeRfmReader{res: res})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", adminui.PageSlot(CustomerRfmPageTemplate), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "lapsed") {
		t.Errorf("认不出的分段应原样显示（不渲染成空白）：页面里没有 lapsed")
	}
}

// —— 五、档位映射只留一份：三页共用 customer_badges.html 的片段 ——

// TestCustomerRfmSegmentToneComesFromTemplate RFM 分段徽章的档位按分段键取。
//
// 与列表页的计数徽章共用同一个 <a> 片段，但键域不同（分段键 vs 口径键），所以单独钉一遍：
// vip → ok、potential → info，其余（含「全部」）落 mute。
func TestCustomerRfmSegmentToneComesFromTemplate(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(&fakeRfmReader{res: rfmSample()})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", adminui.PageSlot(CustomerRfmPageTemplate), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm?range=month", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	body := rec.Body.String()

	for _, tc := range []struct{ segment, want string }{
		{`segment=vip`, "ok"},
		{`segment=potential`, "info"},
		{`segment=low_value`, "mute"},
	} {
		re := regexp.MustCompile(`class="badge badge-([a-z]+)" href="[^"]*` + tc.segment + `"`)
		m := re.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("渲染结果里没有 segment=%s 的分段徽章（片段的 <a> 结构变了？）", tc.segment)
		}
		if m[1] != tc.want {
			t.Errorf("分段 %s 的档位应为 %s，实际 %s", tc.segment, tc.want, m[1])
		}
	}

	// 「全部」的 URL 不带 segment，且它是筛选行的第一个徽章（当前选中 → 带 aria-current）。
	reAll := regexp.MustCompile(`class="badge badge-([a-z]+)" href="/admin/customers/rfm\?[^"]*"(?: aria-current="true")?>`)
	if m := reAll.FindStringSubmatch(body); m == nil {
		t.Error("渲染结果里找不到「全部」分段徽章")
	} else if m[1] != "mute" {
		t.Errorf("「全部」分段的档位应为 mute，实际 %s", m[1])
	}
}

// TestUserBadgeToneMappingsLiveOnlyInFragment 档位映射只能有一份。
//
// 三页各写一遍「状态值 → 语义档」时，改一边忘一边不会报错：同一个状态在列表页是绿、
// 在详情页是灰。这条把「映射只留在 customer_badges.html 的片段里」变成断言 ——
// 页面又出现内联 tone 变量，或者漏了片段引入，当场红。
func TestUserBadgeToneMappingsLiveOnlyInFragment(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "templates", "admin", "user")
	for _, name := range []string{"customers.html", "customer_detail.html", "customer_rfm.html"} {
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		text := string(src)

		if !strings.Contains(text, `{{import "customer_badges.html"}}`) {
			t.Errorf("%s 没有引入 customer_badges.html：档位映射回到页面里就等于又多了一份", name)
		}
		if !strings.Contains(text, "{{yield customer") {
			t.Errorf("%s 没有 yield 客户徽章片段", name)
		}
		for _, banned := range []string{"statusTone", "verifyTone", "tabTone"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s 里出现了 %q：档位映射应当只留在 customer_badges.html 的片段里", name, banned)
			}
		}
	}
}
