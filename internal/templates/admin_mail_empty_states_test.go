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
		{"templates", "admin/mail/mail", map[string]any{"Accounts": []any{}, "Templates": []any{}}, 1,
			"还没有邮件模板", "新建邮件模板，填写主题与正文。", false},
		{"contacts/no-filter", "admin/mail/mail_marketing", marketingProbeData(nil), 0,
			"还没有联系人", "在下方导入联系人，开始建立发送名单。", false},
		{"contacts/keyword", "admin/mail/mail_marketing", marketingProbeData(map[string]any{"Keyword": "nobody"}), 0,
			"没有匹配的联系人", "可调整筛选条件，或在下方折叠区批量导入联系人。", true},
		{"contacts/status", "admin/mail/mail_marketing", marketingProbeData(map[string]any{"Status": "pending"}), 0,
			"没有匹配的联系人", "可调整筛选条件，或在下方折叠区批量导入联系人。", true},
	}
	block := regexp.MustCompile(`(?s)<p class="empty-title">(.*?)</p>\s*<p class="empty-desc">(.*?)</p>`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderAdminEmptyProbe(t, tc.tpl, tc.data)
			matches := block.FindAllStringSubmatch(out, -1)
			if len(matches) < 2 {
				t.Fatalf("空态数量 = %d，期望至少 2", len(matches))
			}
			if got := strings.TrimSpace(matches[tc.index][1]); got != tc.title {
				t.Errorf("标题 = %q，期望 %q", got, tc.title)
			}
			if got := strings.TrimSpace(matches[tc.index][2]); got != tc.desc {
				t.Errorf("引导 = %q，期望 %q", got, tc.desc)
			}
			if tc.tpl == "admin/mail/mail_marketing" {
				clear := strings.Contains(out, `href="/admin/mail/marketing">清空筛选看全部</a>`)
				if clear != tc.clear {
					t.Errorf("清空筛选动作 = %t，期望 %t", clear, tc.clear)
				}
			}
		})
	}
}
