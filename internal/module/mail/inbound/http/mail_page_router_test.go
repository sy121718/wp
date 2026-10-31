package mailhttp

import (
	"testing"

	"go_wp/internal/permission"
)

func TestMailPageObjectsAreDeclared(t *testing.T) {
	declareMailPageObjects()

	cases := []struct {
		perm permission.Perm
		obj  string
	}{
		{permission.MailContactSave, "/api/mail/contact/save"},
		{permission.MailContactDelete, "/api/mail/contact/delete"},
		{permission.MailContactTag, "/api/mail/contact/tag"},
		{permission.MailContactStatus, "/api/mail/contact/status"},
		{permission.MailContactImport, "/api/mail/contact/import"},
		{permission.MailCampaignSave, "/api/mail/campaign/save"},
		{permission.MailCampaignStart, "/api/mail/campaign/start"},
		{permission.MailCampaignDelete, "/api/mail/campaign/delete"},
		{permission.MailAccountSave, "/api/mail/account/save"},
		{permission.MailTemplateSave, "/api/mail/template/save"},
	}
	for _, c := range cases {
		routes := permission.RoutesOf(c.perm)
		if len(routes) == 0 {
			t.Errorf("权限点 %s 没有任何路由声明 —— 用到它的 AI 工具会被判越权（fail closed）", c.perm)
			continue
		}
		found := false
		for _, r := range routes {
			if r.Path == c.obj && r.Method == "POST" {
				found = true
			}
		}
		if !found {
			t.Errorf("权限点 %s 的声明里没有 POST %s，实得 %+v", c.perm, c.obj, routes)
		}
	}
}

func TestDeclareMailPageObjectsIsIdempotent(t *testing.T) {
	declareMailPageObjects()
	declareMailPageObjects()
}
