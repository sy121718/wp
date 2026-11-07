package userhttp

import (
	pageview "go_wp/pkg/view"
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

func TestCustomerRowCarriesFactsNotPresentation(t *testing.T) {
	row := customerRow(customerSample())

	typ := reflect.TypeOf(row)

	for _, banned := range []string{
		"DisplayLabel",
		"StatusTone",
		"EmailVerifiedTone",
		"DetailURL",
		"RegisteredAtText",
		"LastLoginTimeText",
		"LockedUntilText",
	} {
		if _, ok := typ.FieldByName(banned); ok {
			t.Errorf("customerRow 又预制了展示字段 %q：展示判断归模板（本批口径）", banned)
		}
	}
	for _, want := range []string{
		"DisplayName", "Nickname", "Username", "Status", "EmailVerified",
		"RegisteredAt", "LastLoginTime", "LockedUntil",

		"StatusLabelKey", "StatusLabelFallback", "EmailVerifiedKey", "EmailVerifiedFallback",

		"Actionable", "NextStatus", "StatusActionKey", "StatusActionLabel",
	} {
		if _, ok := typ.FieldByName(want); !ok {
			t.Errorf("customerRow 缺少事实字段 %q", want)
		}
	}

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

	if _, ok := row.RegisteredAt.(*utils.JSONTime); !ok {
		t.Errorf("RegisteredAt 应是 *utils.JSONTime（交给模板格式化），实得 %T", row.RegisteredAt)
	}
}

func TestCustomerCounterTabsCarryNoTone(t *testing.T) {
	tabs := customerCounterTabs(userdto.CustomerCounters{Total: 3, Active: 1, Disabled: 1, Pending: 1},
		customerFilter{Status: customerStatusAll, EmailVerified: userdto.EmailVerifiedAll})
	if len(tabs) != 7 {
		t.Fatalf("计数徽章应有 7 个，实得 %d", len(tabs))
	}

	typ := reflect.TypeOf(tabs[0])
	for _, banned := range []string{"Badge", "Tone", "Class", "StatusClass"} {
		if _, ok := typ.FieldByName(banned); ok {
			t.Errorf("计数徽章的色调不该由 Go 给：类型上出现了字段 %q", banned)
		}
	}
}

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

func TestCustomerListStatusToneComesFromTemplate(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   string
		label  string
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

func TestCustomerCounterToneComesFromTemplate(t *testing.T) {
	body := renderCustomerAdminTemplate(t, "admin/user/customers.html", customerTestLayoutData(
		customerListPageData(customerTestPageTitle, customerListSample(), customerFilter{Status: customerStatusAll}, 1, 20, customerMsg{}, false)))

	cases := []struct{ url, tone string }{
		{"/admin/customers", "mute"},
		{"/admin/customers?status=1", "ok"},
		{"/admin/customers?status=0", "danger"},
		{"/admin/customers?status=2", "warn"},
		{"/admin/customers?locked=1", "info"},
		{"/admin/customers?emailVerified=1", "mute"},
		{"/admin/customers?emailVerified=2", "mute"},
	}
	for _, tc := range cases {
		want := `<a class="badge badge-` + tc.tone + `" href="` + tc.url + `"`
		if !strings.Contains(body, want) {
			t.Errorf("计数徽章 %s 的色调应为 %s，渲染结果里没有 %q", tc.url, tc.tone, want)
		}
	}
}

func TestCustomerDetailToneComesFromTemplate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"正常", 1, `badge-ok">正常`},
		{"已停用", 0, `badge-danger">已停用`},
		{"待激活", 2, `badge-warn">待激活`},

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

func TestCustomerRfmUnknownSegmentRendersRawKey(t *testing.T) {
	res := rfmSample()
	res.Items = res.Items[:1]
	res.Items[0].Segment = "lapsed"

	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil,
		fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(&fakeRfmReader{res: res})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/customers/rfm", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("响应 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "lapsed") {
		t.Errorf("认不出的分段应原样显示（不渲染成空白）：页面里没有 lapsed")
	}
}

func TestCustomerRfmSegmentToneComesFromTemplate(t *testing.T) {
	h := NewCustomerPageHandle(&fakeCustomerAdmin{list: customerListSample()}, nil, fakeProjects{items: detailProjects()})
	h.SetCustomerRfm(&fakeRfmReader{res: rfmSample()})
	engine := newCustomerTestEngine(h)
	engine.GET("/admin/customers/rfm", pageview.Slot(CustomerRfmPageTemplate, pageview.RenderPage), h.CustomerRfmPage)

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

	reAll := regexp.MustCompile(`class="badge badge-([a-z]+)" href="/admin/customers/rfm\?[^"]*"(?: aria-current="true")?>`)
	if m := reAll.FindStringSubmatch(body); m == nil {
		t.Error("渲染结果里找不到「全部」分段徽章")
	} else if m[1] != "mute" {
		t.Errorf("「全部」分段的档位应为 mute，实际 %s", m[1])
	}
}

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
