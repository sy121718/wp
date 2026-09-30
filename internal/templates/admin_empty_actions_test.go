package templates

// admin_empty_actions_test.go — 空态第三段（`.empty-actions`）的渲染级守卫（审计 02-L §2 P1-15）。
//
// 为什么需要它：空态只有 title（有时还有 desc）时，用户看完「这里什么都没有」之后
// **没有任何可点的下一步** —— 而下一步往往就在另一个页面（菜单管理 / 页面管理 / 访问统计）。
// 这类缺口不会让任何既有测试变红，HTTP 也恒为 200，只能靠「恰好在那页构造出空数据」才看得见。
//
// 判据分三类，每条都要同时成立：
//  1. 该页空态里**出现** `.empty-actions`（渲染级，静态扫描认不出「这段 HTML 属不属于空态」）；
//  2. 动作指向该页真正的下一步（断言 href / data-drawer-open 的值，防止塞一个占位按钮）；
//  3. 空态仍然落在 `<tbody>` 的 `colspan` 行里、表头仍在（与 admin_list_empty_state_test.go 同源）。
//
// 还有两组**刻意不给**的档位（analytics 的「还没有站点工程」、seo 的体检占位空态、
// mail_marketing 没筛选时的联系人空态）：它们各自的下一步要么已经在句子里给了链接、
// 要么就在紧邻的上方。这些档位用「不出现 .empty-actions」钉住，免得后来者
// 「补齐三段式」时又加回去。
//
// 数据形状取自各页 handler 的真实注入键（`adminShellData()` + 页面自己的键），
// 所以模板里新增/改名的键会在这里以渲染失败的形式暴露，而不是静默漏到线上。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAdminEmptyActionI18nKeysSeeded 空态动作的新词条必须中英成对落进 415 迁移，且真的被模板取用。
//
// 模板里的中文只是 t() 兜底：词条命中时显示的是库里的值，缺 en-US 行时英文界面上永远是中文。
// 反方向同样重要 —— 写了词条却没人取，就是词条表里的一堆死行（孤儿词条）。
func TestAdminEmptyActionI18nKeysSeeded(t *testing.T) {
	// key → 应该在哪个模板里被取用（一一对应，避免「词条在、但取词点被误删」）。
	wantKeys := map[string]string{
		"admin.roles.perm.empty.action":                   "role_permissions.html",
		"admin.seo.paths.empty.action":                    "seo.html",
		"admin.analytics.no_project.refresh":              "analytics.html",
		"admin.analytics.empty.action":                    "analytics.html",
		"admin.mail.automation_run.timeline.empty.action": "mail_automation_run.html",
		"admin.mail.campaign.empty.action":                "mail_campaign.html",
		"admin.redirect.empty.action":                     "page_redirects.html",
		"admin.redirect.pick_project.title":               "page_redirects.html",
		"admin.redirect.pick_project.action":              "page_redirects.html",
		"admin.mail.marketing.contacts.empty.clear":       "mail_marketing.html",
	}

	src, err := os.ReadFile(filepath.Join("..", "..", "public", "migrations", "415_i18n_empty_actions.sql"))
	if err != nil {
		t.Fatalf("读 415 迁移失败: %v", err)
	}
	seedRe := regexp.MustCompile(`\('([^']+)',\s*'(zh-CN|en-US)'`)
	langs := map[string]map[string]bool{}
	for _, m := range seedRe.FindAllStringSubmatch(string(src), -1) {
		if langs[m[1]] == nil {
			langs[m[1]] = map[string]bool{}
		}
		langs[m[1]][m[2]] = true
	}

	for key, tpl := range wantKeys {
		got := langs[key]
		if got == nil {
			t.Errorf("词条 %q 不在 415 迁移里：英文界面上会一直显示中文", key)
			continue
		}
		if !got["zh-CN"] || !got["en-US"] {
			t.Errorf("词条 %q 语言不成对：zh-CN=%v en-US=%v", key, got["zh-CN"], got["en-US"])
		}
		// 短名 → 实际路径（模板已按后端模块分进子目录），别硬编码 admin/ 下的一层路径。
		body := adminTemplateSource(t, tpl)
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("词条 %q 在 %s 里没有取词点（词条与模板失去同步）", key, tpl)
		}
	}
}

// TestAdminEmptyActionKeysSatisfyMigrationGate 415 的幂等条件按「本批 en-US 行数」判定，
// 门槛数字必须与迁移文件里的 key 数一致 —— 写小了会让整批词条被静默跳过，
// 写大了会让这条 seed 每次启动都重跑。
func TestAdminEmptyActionKeysSatisfyMigrationGate(t *testing.T) {
	sqlSrc, err := os.ReadFile(filepath.Join("..", "..", "public", "migrations", "415_i18n_empty_actions.sql"))
	if err != nil {
		t.Fatalf("读 415 迁移失败: %v", err)
	}
	goSrc, err := os.ReadFile(filepath.Join("..", "..", "public", "migrations", "register_empty_actions_i18n.go"))
	if err != nil {
		t.Fatalf("读 415 注册文件失败: %v", err)
	}

	seedRe := regexp.MustCompile(`\('([^']+)',\s*'en-US'`)
	enKeys := map[string]bool{}
	for _, m := range seedRe.FindAllStringSubmatch(string(sqlSrc), -1) {
		enKeys[m[1]] = true
	}

	gateRe := regexp.MustCompile(`COUNT\(\*\) >= (\d+)`)
	gate := gateRe.FindStringSubmatch(string(goSrc))
	if gate == nil {
		t.Fatal("注册文件里没有 COUNT(*) >= N 的幂等条件")
	}
	if gate[1] != itoaLen(len(enKeys)) {
		t.Errorf("幂等条件的门槛是 %s，但迁移里有 %d 个 en-US 词条 —— 两者必须相等", gate[1], len(enKeys))
	}
}

// itoaLen 把很小的非负整数转成字符串（用例里只用于比对门槛数字）。
func itoaLen(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// countEmptyActions 数出输出里 `.empty-actions` 的出现次数。
func countEmptyActions(out string) int {
	return strings.Count(out, `class="empty-actions"`)
}

// assertEmptyActions 断言空态里出现恰好 n 个 .empty-actions，且动作指向 wantHref（空串表示不校验）。
func assertEmptyActions(t *testing.T, label, out string, n int, wantHref string) {
	t.Helper()
	if got := countEmptyActions(out); got != n {
		t.Errorf("%s：期望 %d 处 .empty-actions，实际 %d 处", label, n, got)
	}
	if wantHref != "" && !strings.Contains(out, wantHref) {
		t.Errorf("%s：空态动作没有指向 %q", label, wantHref)
	}
}

// withKeys 在基础数据上覆盖若干键（用例里只写「与默认不同的那一小撮」）。
func withKeys(base, over map[string]any) map[string]any {
	for k, v := range over {
		base[k] = v
	}
	return base
}

// analyticsProbeData 访问统计页的完整渲染数据（模板里的必需键一个都不能少：
// 缺键会让 Jet 在那一行中断，而 HTTP 仍是 200 —— 后半页整块消失）。
func analyticsProbeData(over map[string]any) map[string]any {
	return withKeys(map[string]any{
		"Projects": []any{}, "SelectedProject": "", "FilterFrom": "", "FilterTo": "",
		"RangeFrom": "", "RangeTo": "", "Total": 0, "Visitors": 0,
		"Daily": []any{}, "Paths": []any{}, "PathTotal": 0,
		"Referrers": []any{}, "UAClasses": []any{}, "Langs": []any{},
		"RankLimit": 10, "BreakdownUnavailable": false, "Err": "", "LoadFailed": false,
	}, over)
}

// redirectProbeData 重定向页的渲染数据。该页**不继承 layout**（自带最小外壳），
// 所有键由 handler 的 redirectPageData 预置 —— 这里照抄同一份形状。
func redirectProbeData(over map[string]any) map[string]any {
	return withKeys(map[string]any{
		"Title": "重定向管理", "PagePath": "/api/page/redirect",
		"OkKey": "", "ErrKey": "", "Done": "", "ErrText": "",
		"CreateSource": "", "CreateTarget": "", "CreateFailed": false,
		"Projects": []any{map[string]any{"ID": "pr1", "Name": "官网"}}, "SelectedProject": "pr1",
		"Items": []any{}, "Total": 0, "EffectiveCount": 0, "MultiHopCount": 0, "LoopCount": 0,
	}, over)
}

// marketingProbeData 邮件营销页的渲染数据。
func marketingProbeData(over map[string]any) map[string]any {
	return withKeys(map[string]any{
		"Contacts": []any{}, "ContactTotal": int64(0),
		"Campaigns": []any{}, "CampaignTotal": int64(0),
		"Accounts": []any{}, "Templates": []any{},
		"Page": 1, "Keyword": "", "Status": "",
		// 有「新建活动」权限：活动表的空态才带动作 —— 这样「联系人不给动作」那两条用例
		// 数出来的 .empty-actions 就只可能来自活动表。
		"PermSet": map[string]any{"mail:campaign_save": true},
	}, over)
}

// TestAdminEmptyStateHasActions 逐页断言空态的第三段存在且指向真正的下一步。
func TestAdminEmptyStateHasActions(t *testing.T) {
	t.Run("departments/有新建权限→新建部门按钮", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/departments", map[string]any{
			"Rows": []any{}, "Parents": []any{}, "PermSet": map[string]any{"dept:create": true},
		})
		assertEmptyKeepsTableHead(t, "departments", out, "7", []string{"还没有部门"})
		assertEmptyActions(t, "departments", out, 1, `data-drawer-open="#tpl-dept-create"`)
	})

	t.Run("departments/无新建权限→不给动作", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/departments", map[string]any{"Rows": []any{}, "Parents": []any{}})
		assertEmptyActions(t, "departments(无权限)", out, 0, "")
	})

	t.Run("menus/有新建权限→新建菜单按钮", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/menus", map[string]any{
			"Rows": []any{}, "Parents": []any{}, "PermSet": map[string]any{"menu:create": true},
		})
		assertEmptyKeepsTableHead(t, "menus", out, "10", []string{"还没有菜单"})
		assertEmptyActions(t, "menus", out, 1, `data-drawer-open="#tpl-menu-create"`)
	})

	t.Run("role_permissions→去菜单管理", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/role_permissions", map[string]any{
			"Rows": []any{}, "Role": map[string]any{"RoleID": 1, "RoleName": "内容编辑", "RoleCode": "editor", "MenuIDs": []any{}},
		})
		assertEmptyActions(t, "role_permissions", out, 1, `href="/admin/menus"`)
	})

	t.Run("seo/热门路径→去访问统计（带工程参数）", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/analytics/seo", map[string]any{
			"Projects":        []any{map[string]any{"ID": "pr1", "Name": "官网"}},
			"SelectedProject": "pr1", "Paths": []any{}, "HasPaths": false,
			"AnalyticsError": false, "PermSet": map[string]any{"seo:audit": true},
		})
		// 体检占位那一处刻意不给动作，所以合计恰好 1 处（热门路径的）。
		assertEmptyActions(t, "seo", out, 1, `href="/admin/analytics?project=pr1"`)
	})

	t.Run("analytics/装载失败档→刷新重试", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/analytics/analytics", analyticsProbeData(map[string]any{
			"Projects": []any{}, "SelectedProject": "", "LoadFailed": true,
		}))
		assertEmptyActions(t, "analytics/loadFailed", out, 1, `href=""`)
	})

	t.Run("analytics/还没有工程→刻意不给动作（句子里已有链接）", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/analytics/analytics", analyticsProbeData(map[string]any{
			"Projects": []any{}, "SelectedProject": "",
		}))
		assertEmptyActions(t, "analytics/noProject", out, 0, "")
		if !strings.Contains(out, `href="/admin/pages"`) {
			t.Error("analytics：无工程档的句内「页面管理」链接应仍在")
		}
	})

	t.Run("analytics/五张维度表都空→各给一处动作", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/analytics/analytics", analyticsProbeData(map[string]any{
			"Projects":        []any{map[string]any{"ID": "pr1", "Name": "官网"}},
			"SelectedProject": "pr1",
		}))
		// 按天 / 路径 / 来源 / 设备 / 语言 = 5 处，全部指向发布页。
		assertEmptyActions(t, "analytics/empty", out, 5, `href="/admin/pages"`)
	})

	t.Run("analytics/明细被保留期清理→三个维度榜不给动作", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/analytics/analytics", analyticsProbeData(map[string]any{
			"Projects":        []any{map[string]any{"ID": "pr1", "Name": "官网"}},
			"SelectedProject": "pr1", "Total": 12, "BreakdownUnavailable": true,
		}))
		// 按天 / 路径来自汇总（仍给动作），来源 / 设备 / 语言来自明细（不给）→ 合计 2 处。
		assertEmptyActions(t, "analytics/retention", out, 2, `href="/admin/pages"`)
	})

	t.Run("mail_automation_run→刷新", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_automation_run", map[string]any{
			"Err": "",
			"D": map[string]any{
				"Explain": "流程：欢迎邮件", "Timeline": []any{}, "DoneNodes": 0, "TotalNodes": 3,
				"Run": map[string]any{"Status": "pending"},
			},
		})
		// 时间线空态仍落在 tbody 的 colspan=5 行里（表头不消失）。
		assertEmptyKeepsTableHead(t, "mail_automation_run", out, "5", []string{"还没有执行记录"})
		assertEmptyActions(t, "mail_automation_run", out, 1, `href=""`)
	})

	t.Run("mail_campaign/链接与收件人都为空→回营销页", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_campaign", map[string]any{
			"Err": "",
			"R": map[string]any{
				"Campaign": map[string]any{"Name": "九月活动", "Status": "draft", "Subject": "上新"},
				"Links":    []any{}, "Recipients": []any{}, "Total": 0,
			},
		})
		// 两张表的空态各自落在 tbody 里：链接排行 colspan=3、收件人明细 colspan=7。
		assertEmptyKeepsTableHead(t, "mail_campaign", out, "3", []string{`colspan="7"`})
		assertEmptyActions(t, "mail_campaign", out, 2, `href="/admin/mail/marketing"`)
	})

	t.Run("page_redirects/列表为空→去页面管理改 URL", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/page/page_redirects", redirectProbeData(map[string]any{"Items": []any{}}))
		assertEmptyKeepsTableHead(t, "page_redirects", out, "7", []string{"还没有重定向"})
		assertEmptyActions(t, "page_redirects", out, 1, `href="/admin/pages"`)
	})

	t.Run("page_redirects/没有工程→补 title 与建工程入口", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/page/page_redirects", redirectProbeData(map[string]any{
			"Projects": []any{}, "SelectedProject": "",
		}))
		assertEmptyActions(t, "page_redirects/noProject", out, 1, `href="/admin/pages"`)
		if !strings.Contains(out, "还没有可管理的站点工程") {
			t.Error("page_redirects：未选到工程档应补上 title（原先只有 desc）")
		}
	})

	t.Run("mail_marketing/联系人带筛选→清空筛选", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_marketing", marketingProbeData(map[string]any{
			"Contacts": []any{}, "Keyword": "nobody", "Status": "subscribed",
		}))
		// 联系人（清空筛选）+ 活动（新建活动）= 2 处。
		assertEmptyActions(t, "mail_marketing/contacts", out, 2, `href="/admin/mail/marketing"`)
	})

	t.Run("mail_marketing/联系人无筛选→不给动作", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_marketing", marketingProbeData(map[string]any{
			"Contacts": []any{},
		}))
		// 只剩活动表那一处。
		assertEmptyActions(t, "mail_marketing/noFilter", out, 1, "")
	})
}
