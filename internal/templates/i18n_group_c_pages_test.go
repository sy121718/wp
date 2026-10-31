package templates

import (
	"testing"

	"github.com/CloudyKit/jet/v6"
)

func TestGroupCPagesParse(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	files := []string{
		"admin/order/coupons", "admin/order/returns", "admin/order/orders", "admin/user/customers",
		"admin/user/customer_detail", "admin/mail/mail", "admin/mail/mail_templates",
		"admin/mail/mail_contacts", "admin/mail/mail_campaigns", "admin/mail/mail_campaign",
		"admin/mail/mail_automation", "admin/mail/mail_automation_runs",
		"admin/mail/mail_automation_run", "admin/mail/mail_automation_canvas",
	}
	for _, f := range files {
		if _, err := set.GetTemplate(f); err != nil {
			t.Fatalf("%s 解析失败: %v", f, err)
		}
	}
}
