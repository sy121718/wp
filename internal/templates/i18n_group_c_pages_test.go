package templates

// i18n_group_c_pages_test.go — 营销 / 订单 / 客户 / 邮件四组后台模板的 Jet 语法契约。
//
// 这些页面大多没有 handler 渲染测试兜底（customers / customer_detail / returns 除外），
// 模板写错只会在运营点开页面时暴露。这里钉住「十二个模板都能被 Jet 解析」，
// 覆盖 {{ .["t"]("key", "兜底") }} 与 range 内 {{tr := .["t"]}} 两条 i18n 取词路径。
import (
	"testing"

	"github.com/CloudyKit/jet/v6"
)

func TestGroupCPagesParse(t *testing.T) {
	loader := jet.NewOSFileSystemLoader(".")
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	files := []string{
		"admin/coupons", "admin/returns", "admin/orders", "admin/customers",
		"admin/customer_detail", "admin/mail", "admin/mail_marketing", "admin/mail_campaign",
		"admin/mail_automation", "admin/mail_automation_edit", "admin/mail_automation_run",
		"admin/mail_automation_canvas",
	}
	for _, f := range files {
		if _, err := set.GetTemplate(f); err != nil {
			t.Fatalf("%s 解析失败: %v", f, err)
		}
	}
}
