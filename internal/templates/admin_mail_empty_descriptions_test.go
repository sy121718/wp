package templates

import (
	"regexp"
	"strings"
	"testing"
)

func TestMailEmptyDescriptionsExplainNextStep(t *testing.T) {
	cases := []struct {
		name, tpl string
		data      map[string]any
		pairs     [][2]string
	}{
		{
			name: "marketing", tpl: "admin/mail/mail_marketing",
			data: marketingProbeData(map[string]any{"Keyword": "nobody"}),
			pairs: [][2]string{
				{"没有匹配的联系人", "可调整筛选条件，或在下方折叠区批量导入联系人。"},
				{"还没有活动", "新建活动后点「启动群发」开始发送。"},
			},
		},
		{
			name: "campaign", tpl: "admin/mail/mail_campaign",
			data: map[string]any{"Err": "", "R": map[string]any{
				"Campaign": map[string]any{"Name": "活动", "Status": "draft", "Subject": "主题"},
				"Links":    []any{}, "Recipients": []any{}, "Total": 0,
			}},
			pairs: [][2]string{
				{"还没有点击数据", "启动群发后，有收件人点击邮件链接才会显示排行。"},
				{"还没有投递记录", "启动群发后，收件人会分批进入投递队列。"},
			},
		},
		{
			name: "automation", tpl: "admin/mail/mail_automation",
			data: map[string]any{
				"Err": "", "Ok": "", "AutoTotal": 0, "Automations": []any{},
				"Runs": []any{}, "RunTotal": 0, "FilterID": "", "FilterRun": "",
			},
			pairs: [][2]string{
				{"还没有流程", "先新建一条，比如「新订阅 → 等 1 天 → 发欢迎邮件 → 打上 welcomed 标签」。"},
				{"还没有实例", "流程启用后，满足触发条件的人会自动进入。"},
			},
		},
	}
	block := regexp.MustCompile(`(?s)<p class="empty-title">(.*?)</p>\s*<p class="empty-desc">(.*?)</p>`)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderAdminEmptyProbe(t, tc.tpl, tc.data)
			matches := block.FindAllStringSubmatch(out, -1)
			if len(matches) != len(tc.pairs) {
				t.Fatalf("空态数量 = %d，期望 %d", len(matches), len(tc.pairs))
			}
			for i, pair := range tc.pairs {
				if title, desc := strings.TrimSpace(matches[i][1]), strings.TrimSpace(matches[i][2]); title != pair[0] || desc != pair[1] {
					t.Errorf("空态 %d: title=%q desc=%q，期望 %q / %q", i, title, desc, pair[0], pair[1])
				}
			}
		})
	}
}
