package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	"go_wp/internal/adminui"
	"strings"
	"testing"

	i18n "go_wp/pkg/i18n"

	mailhttp "go_wp/internal/module/mail/inbound/http"
)

const (
	sentinelContactsEmpty  = "SENTINEL_CONTACTS_EMPTY_DESC"
	sentinelCampaignBack   = "SENTINEL_CAMPAIGN_BACK"
	sentinelCampaignAction = "SENTINEL_CAMPAIGN_EMPTY_ACTION"
	sentinelHelpLabel      = "SENTINEL_HELP_LABEL"
)

type mailI18nSentinelCase struct {
	name     string
	path     string
	key      string
	sentinel string
	fallback string
}

func TestMailPagesRenderDBValueInsteadOfFallback(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}

	if err := f.seedCampaign(context.Background(), "i18n 哨兵活动"); err != nil {
		t.Fatalf("造活动失败: %v", err)
	}
	var campaignID int64
	if err := f.db.Raw("SELECT id FROM mail_campaigns ORDER BY id DESC LIMIT 1").
		Scan(&campaignID).Error; err != nil || campaignID == 0 {
		t.Fatalf("取活动 id 失败: err=%v id=%d", err, campaignID)
	}

	engine := mailSplitEngine(t)
	handle := mailhttp.NewMailPageHandle(f.svc)
	engine.GET("/admin/mail/contacts",
		pageview.Slot(mailhttp.ContactsPageTemplate, adminui.RenderPage), handle.MailContactsPage)
	engine.GET("/admin/mail/templates",
		pageview.Slot(mailhttp.TemplatesPageTemplate, adminui.RenderPage), handle.MailTemplatesPage)
	engine.GET("/admin/mail/campaign",
		pageview.Slot(mailhttp.CampaignPageTemplate, adminui.RenderPage), handle.MailCampaignPage)

	cases := []mailI18nSentinelCase{
		{

			name:     "联系人页筛选空态描述",
			path:     "/admin/mail/contacts?keyword=definitely-no-such-contact",
			key:      "admin.mail.marketing.contacts.empty",
			sentinel: sentinelContactsEmpty,
			fallback: "可调整筛选条件，或点右上角「导入联系人」批量导入。",
		},
		{
			name:     "活动报表页返回按钮",
			path:     "/admin/mail/campaign?id=" + itoa(campaignID),
			key:      "admin.mail.campaign.back",
			sentinel: sentinelCampaignBack,
			fallback: "返回群发活动",
		},
		{

			name:     "活动报表页空态动作",
			path:     "/admin/mail/campaign?id=" + itoa(campaignID),
			key:      "admin.mail.campaign.empty.action",
			sentinel: sentinelCampaignAction,
			fallback: "回群发活动页启动",
		},
		{
			name:     "邮件模板页帮助按钮",
			path:     "/admin/mail/templates",
			key:      "admin.common.help.label",
			sentinel: sentinelHelpLabel,
			fallback: "查看说明",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			i18n.InjectForTest(nil, nil)
			before := mailSplitGET(t, engine, tc.path).Body.String()
			if !strings.Contains(before, tc.fallback) {
				t.Fatalf("缓存为空时正文没有出现 fallback %q：对照不成立，后面的断言分辨不了两条路径", tc.fallback)
			}
			if strings.Contains(before, tc.sentinel) {
				t.Fatalf("缓存为空时正文出现了哨兵 %q：注入尚未发生，说明前一个用例没复位缓存", tc.sentinel)
			}

			i18n.InjectForTest(map[string]map[string]string{
				tc.key: {"zh-CN": tc.sentinel, "en-US": tc.sentinel},
			}, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			after := mailSplitGET(t, engine, tc.path).Body.String()
			if !strings.Contains(after, tc.sentinel) {
				t.Errorf("注入词条后正文没有出现哨兵 %q：渲染没有走 DB 值路径（页面可能根本没引用 %s）",
					tc.sentinel, tc.key)
			}
			if strings.Contains(after, tc.fallback) {
				t.Errorf("注入词条后正文仍出现 fallback %q：键存在却没命中取值链 —— "+
					"正是「模板改了、库里没改」这一轮三个缺陷的形态", tc.fallback)
			}
		})
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
