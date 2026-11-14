package templates

import (
	"go_wp/pkg/facing"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAdminEmptyActionI18nKeysSeeded(t *testing.T) {

	wantKeys := map[string]string{
		"admin.roles.perm.empty.action":                   "grant_menu_tree.html",
		"admin.seo.paths.empty.action":                    "seo.html",
		"admin.analytics.empty.action":                    "analytics.html",
		"admin.mail.automation_run.timeline.empty.action": "mail_automation_run.html",
		"admin.mail.campaign.empty.action":                "mail_campaign.html",
		"admin.redirect.empty.action":                     "page_redirects.html",
		"admin.redirect.pick_project.title":               "page_redirects.html",
		"admin.redirect.pick_project.action":              "page_redirects.html",
		"admin.mail.marketing.contacts.empty.clear":       "mail_contacts.html",
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

		body := adminTemplateSource(t, tpl)
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("词条 %q 在 %s 里没有取词点（词条与模板失去同步）", key, tpl)
		}
	}
}

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

func countEmptyActions(out string) int {
	return strings.Count(out, `class="empty-actions"`)
}

func assertEmptyActions(t *testing.T, label, out string, n int, wantHref string) {
	t.Helper()
	if got := countEmptyActions(out); got != n {
		t.Errorf("%s：期望 %d 处 .empty-actions，实际 %d 处", label, n, got)
	}
	if wantHref != "" && !strings.Contains(out, wantHref) {
		t.Errorf("%s：空态动作没有指向 %q", label, wantHref)
	}
}

func withKeys(base, over map[string]any) map[string]any {
	for k, v := range over {
		base[k] = v
	}
	return base
}

func analyticsProbeData(over map[string]any) map[string]any {
	return withKeys(map[string]any{
		"Projects": []any{}, "SelectedProject": "", "FilterFrom": "", "FilterTo": "",
		"RangeFrom": "", "RangeTo": "", "Total": 0, "Visitors": 0,
		"Daily": []any{}, "Paths": []any{}, "PathTotal": 0,
		"Referrers": []any{}, "UAClasses": []any{}, "Langs": []any{},
		"RankLimit": 10, "BreakdownUnavailable": false,
	}, over)
}

func redirectProbeData(over map[string]any) map[string]any {
	return withKeys(map[string]any{
		"Title": "重定向管理", "PagePath": "/api/page/redirect",
		"OkKey": "", "ErrKey": "", "Done": "", "ErrText": "",
		"CreateSource": "", "CreateTarget": "", "CreateFailed": false,
		"Projects": []any{map[string]any{"ID": "pr1", "Name": "官网"}}, "SelectedProject": "pr1",
		"Items": []any{}, "Total": 0, "EffectiveCount": 0, "MultiHopCount": 0, "LoopCount": 0,
	}, over)
}

func marketingProbeData(over map[string]any) map[string]any {
	return withKeys(map[string]any{
		"Contacts": []any{}, "ContactTotal": int64(0),
		"Campaigns": []any{}, "CampaignTotal": int64(0),
		"Accounts": []any{}, "Templates": []any{},
		"Page": 1, "Keyword": "", "Status": "",

		"Buttons": map[string]any{"mail.campaign_save": true},
	}, over)
}

func TestAdminEmptyStateHasActions(t *testing.T) {
	t.Run("departments/有新建权限→新建部门按钮", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/system/departments", map[string]any{
			"Rows": []any{}, "Parents": []any{}, "Buttons": map[string]any{"dept.create": true},
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
			"Rows": []any{}, "Parents": []any{}, "Buttons": map[string]any{"menu.create": true},
		})
		assertEmptyKeepsTableHead(t, "menus", out, "9", []string{"还没有菜单"})
		assertEmptyActions(t, "menus", out, 1, `data-drawer-open="#tpl-menu-create"`)
	})

	// 权限抽屉改为按需加载的片段（GET /admin/roles/permissions/drawer），
	// 空态动作必须跟着搬进 grant_menu_tree.html，否则「没有可分配菜单」时
	// 不再给出「去菜单管理」入口。
	t.Run("权限抽屉片段→空态仍指向菜单管理", func(t *testing.T) {
		out := adminTemplateSource(t, "grant_menu_tree.html")
		if !strings.Contains(out, `admin.roles.perm.empty.action`) {
			t.Fatal("grant_menu_tree.html 里没有空态动作词条")
		}
		if !strings.Contains(out, `href="/admin/menus"`) {
			t.Error("权限抽屉空态缺少「去菜单管理」入口")
		}
		if !strings.Contains(out, `"admin.roles.perm.empty.action"`) {
			t.Error("权限抽屉空态缺少取词点")
		}
	})

	t.Run("seo/热门路径→去访问统计（带工程参数）", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/analytics/seo", map[string]any{
			"Projects":        []any{map[string]any{"ID": "pr1", "Name": "官网"}},
			"SelectedProject": "pr1", "Paths": []any{}, "HasPaths": false,
			"Buttons": map[string]any{"seo.audit": true},
		})

		assertEmptyActions(t, "seo", out, 1, `href="/admin/analytics?project=pr1"`)
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

		assertEmptyActions(t, "analytics/empty", out, 5, `href="/admin/pages"`)
	})

	t.Run("analytics/明细被保留期清理→三个维度榜不给动作", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/analytics/analytics", analyticsProbeData(map[string]any{
			"Projects":        []any{map[string]any{"ID": "pr1", "Name": "官网"}},
			"SelectedProject": "pr1", "Total": 12, "BreakdownUnavailable": true,
		}))

		assertEmptyActions(t, "analytics/retention", out, 2, `href="/admin/pages"`)
	})

	t.Run("mail_automation_run→刷新", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_automation_run", map[string]any{
			"Err": facing.Msg{},
			"D": map[string]any{
				"Explain": "流程：欢迎邮件", "Timeline": []any{}, "DoneNodes": 0, "TotalNodes": 3,
				"Run": map[string]any{"Status": "pending"},
			},
		})

		assertEmptyKeepsTableHead(t, "mail_automation_run", out, "5", []string{"还没有执行记录"})
		assertEmptyActions(t, "mail_automation_run", out, 1, `href=""`)
	})

	t.Run("mail_campaign/链接与收件人都为空→回群发活动页", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_campaign", map[string]any{
			"Err": facing.Msg{},
			"R": map[string]any{
				"Campaign": map[string]any{"Name": "九月活动", "Status": "draft", "Subject": "上新"},
				"Links":    []any{}, "Recipients": []any{}, "Total": 0,
			},
		})

		assertEmptyKeepsTableHead(t, "mail_campaign", out, "3", []string{`colspan="7"`})
		assertEmptyActions(t, "mail_campaign", out, 2, `href="/admin/mail/campaigns"`)
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

	t.Run("mail_contacts/联系人带筛选→清空筛选", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_contacts", marketingProbeData(map[string]any{
			"Contacts": []any{}, "Keyword": "nobody", "Status": "subscribed",
		}))

		assertEmptyActions(t, "mail_contacts/contacts", out, 1, `href="/admin/mail/contacts"`)
	})

	t.Run("mail_contacts/联系人无筛选→不给动作", func(t *testing.T) {
		out := renderAdminEmptyProbe(t, "admin/mail/mail_contacts", marketingProbeData(map[string]any{
			"Contacts": []any{},
		}))

		assertEmptyActions(t, "mail_contacts/noFilter", out, 0, "")
	})
}
