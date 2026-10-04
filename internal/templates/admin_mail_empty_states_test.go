package templates

import (
	"regexp"
	"strings"
	"testing"
)

func TestMailEmptyStatesByFilter(t *testing.T) {
	cases := []struct {
		name, tpl string
		data      map[string]any
		index     int
		title     string
		desc      string
		clear     bool
	}{
		{"templates", "admin/mail/mail_templates", map[string]any{"Templates": []any{}}, 0,
			"还没有邮件模板", "新建邮件模板，填写主题与正文。", false},
		// 526 起无筛选空态同时指向「新建联系人」与「导入联系人」两个入口：
		// 只提导入会让「手工加一条」这条路径在页面上没有出口。
		{"contacts/no-filter", "admin/mail/mail_contacts", marketingProbeData(nil), 0,
			"还没有联系人", "新建一位联系人手工加一条，或用「导入联系人」批量导入 CSV —— 名单建立起来才能按标签圈人群群发。", false},
		{"contacts/keyword", "admin/mail/mail_contacts", marketingProbeData(map[string]any{"Keyword": "nobody"}), 0,
			"没有匹配的联系人", "可调整筛选条件，或点右上角「导入联系人」批量导入。", true},
		{"contacts/status", "admin/mail/mail_contacts", marketingProbeData(map[string]any{"Status": "pending"}), 0,
			"没有匹配的联系人", "可调整筛选条件，或点右上角「导入联系人」批量导入。", true},
	}
	block := regexp.MustCompile(`(?s)<p class="empty-title">(.*?)</p>\s*<p class="empty-desc">(.*?)</p>`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderAdminEmptyProbe(t, tc.tpl, tc.data)
			matches := block.FindAllStringSubmatch(out, -1)
			if len(matches) < 1 {
				t.Fatalf("空态数量 = %d，期望至少 1", len(matches))
			}
			if got := strings.TrimSpace(matches[tc.index][1]); got != tc.title {
				t.Errorf("标题 = %q，期望 %q", got, tc.title)
			}
			if got := strings.TrimSpace(matches[tc.index][2]); got != tc.desc {
				t.Errorf("引导 = %q，期望 %q", got, tc.desc)
			}
			// 清空筛选出口必须指向本页地址（拆页前它指向 /admin/mail/marketing，
			// 那样点一次要多绕一跳 302，且旧地址将来撤掉就断链）。
			if strings.HasPrefix(tc.tpl, "admin/mail/mail_contacts") {
				clear := strings.Contains(out, `href="/admin/mail/contacts">清空筛选看全部</a>`)
				if clear != tc.clear {
					t.Errorf("清空筛选动作 = %t，期望 %t", clear, tc.clear)
				}
			}
		})
	}
}
