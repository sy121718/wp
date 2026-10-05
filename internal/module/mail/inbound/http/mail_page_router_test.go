package mailhttp

// mail_page_router_test.go — 页面入口的鉴权对象必须进权限声明表。
//
// 这条测试钉的是一个**只在对 AI 工具调用时才显形**的缺陷：
// 本模块页面写操作复用的是「对应 API 的路径」当 obj（部分 API 路由并不存在，
// 策略表里也只有那条路径），而 permission 的声明表只由「带 permission.X 参数的路由注册」
// 填充 —— 页面路由走 CasbinMiddlewareForPath，登记不了自己。
//
// 实测后果：内部工具调用一律 forbidden（AI 回答「没有权限执行该操作」），
// 而页面本身完全正常，排查时会往「AI 权限没配」的方向找。
//
// 这里只断言**声明表非空**，不查 Casbin 策略：策略是运维数据（迁移 + 后台配置），
// 测试环境里不保证存在；而「连声明都没有」是代码层面的确定性事实。

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

// 重复调用不能 panic（Declare 对同 key 同权限点是幂等的，装配可能被调用多次）。
func TestDeclareMailPageObjectsIsIdempotent(t *testing.T) {
	declareMailPageObjects()
	declareMailPageObjects()
}
