package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

var mailCrudLeakTokens = []string{
	"23505", "duplicate key", "pgx", "mail_contacts", "mail_suppressions", "ERROR:", "SELECT ",
}

const mailCrudUserID = int64(987654311)

func newMailContactCrudRouter(t *testing.T, svc *mailservice.Service, perms map[string]bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", mailCrudUserID)
		c.Next()
		for _, e := range c.Errors {
			t.Logf("gin 渲染错误: %v", e.Err)
		}
	})
	if perms != nil {
		router.Use(func(c *gin.Context) {
			c.Set(adminctx.PermSetKey, perms)
			c.Set(adminctx.ButtonsKey, buttonsOf(perms))
			c.Next()
		})
	}
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	router.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	page := mailhttp.NewMailPageHandle(svc)
	router.GET("/admin/mail/contacts",
		pageview.Slot(mailhttp.ContactsPageTemplate, adminui.RenderPage), page.MailContactsPage)

	api := router.Group("/api/mail")
	write := mailhttp.NewHandle(svc)
	api.POST("/contact/save", pageview.HTMLFragment, write.ContactSave)
	api.POST("/contact/delete", pageview.HTMLFragment, write.ContactDelete)
	api.POST("/contact/tag", pageview.HTMLFragment, write.ContactTag)
	api.POST("/contact/status", pageview.HTMLFragment, write.ContactStatus)
	router.POST("/admin/mail/contact/status", pageview.HTMLFragment, page.MailContactStatus)

	router.POST("/admin/mail/contacts/bulk-status", pageview.HTMLFragment, page.MailContactsBulkStatus)
	router.POST("/admin/mail/contact/import", pageview.HTMLFragment, page.MailContactImport)
	return router
}

func mailCrudPagePost(t *testing.T, router *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// mailCrudWrite 提交标准表单并返回状态码与 HTML 正文；
// 写端点已改为 Jet 片段出口，JSON 断言的旧通道不再存在。
func mailCrudWrite(t *testing.T, router *gin.Engine, path string, form url.Values) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("%s：界面响应必须是 HTML，实际 %q", path, rec.Header().Get("Content-Type"))
	}
	for _, token := range mailCrudLeakTokens {
		if strings.Contains(body, token) {
			t.Fatalf("%s：回执泄漏了内部细节 %q：%s", path, token, firstN(body, 300))
		}
	}
	return rec.Code, body
}

func mailCrudForm(pairs ...string) url.Values {
	form := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		form.Set(pairs[i], pairs[i+1])
	}
	return form
}

func mailCrudContains(t *testing.T, body, label string, want ...string) {
	t.Helper()
	for _, w := range want {
		if strings.Contains(body, w) {
			return
		}
	}
	t.Fatalf("%s：正文不含任何预期线索 %v：%s", label, want, firstN(body, 400))
}

func mailCrudGet(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestMailContactCrudSaveCreatesThenUpdates(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)

	code, body := mailCrudWrite(t, router, "/api/mail/contact/save", mailCrudForm(
		"id", "0", "email", "QA.New@Example.invalid", "name", "测试新人",
		"tags", "vip, 华南", "status", "subscribed", "consent_source", "官网表单勾选同意",
	))
	if code != http.StatusOK {
		t.Fatalf("新建联系人应 200，实际 %d（%s）", code, body)
	}
	mailCrudContains(t, body, "新建联系人", `role="status"`)

	row, err := m.GetContactByEmail(ctx, "qa.new@example.invalid")
	if err != nil {
		t.Fatalf("新建后按邮箱查不到联系人：%v", err)
	}
	if row.Status != mailmodel.ContactStatusSubscribed {
		t.Fatalf("新建状态应为 subscribed，实际 %q", row.Status)
	}
	if row.Source != mailmodel.ContactSourceManual {
		t.Fatalf("手工新建的来源应为 %q，实际 %q", mailmodel.ContactSourceManual, row.Source)
	}
	if row.SubscribedAt == nil {
		t.Fatal("订阅态新建必须写 subscribed_at")
	}
	if len(row.Tags) != 2 || row.Tags[0] != "vip" || row.Tags[1] != "华南" {
		t.Fatalf("标签应切成 [vip 华南]，实际 %v", []string(row.Tags))
	}

	code, body = mailCrudWrite(t, router, "/api/mail/contact/save", mailCrudForm(
		"id", strconv.FormatUint(row.ID, 10), "email", "qa.renamed@example.invalid",
		"name", "改名", "tags", "vip", "status", "", "consent_source", "",
	))
	if code != http.StatusOK {
		t.Fatalf("编辑联系人应 200，实际 %d（%s）", code, body)
	}
	updated, err := m.GetContactByEmail(ctx, "qa.renamed@example.invalid")
	if err != nil {
		t.Fatalf("改邮箱后按新邮箱查不到：%v", err)
	}
	if updated.ID != row.ID {
		t.Fatalf("编辑应改同一行：期望 id=%d，实际 %d", row.ID, updated.ID)
	}
	if updated.Name == nil || *updated.Name != "改名" {
		t.Fatalf("姓名未更新：%v", updated.Name)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "vip" {
		t.Fatalf("标签应为全量覆盖 [vip]，实际 %v", []string(updated.Tags))
	}
	if updated.Status != mailmodel.ContactStatusSubscribed {
		t.Fatalf("表单没提交状态时不该改状态，实际 %q", updated.Status)
	}
	if updated.Source != mailmodel.ContactSourceManual {
		t.Fatalf("表单没提交来源时不该改来源，实际 %q", updated.Source)
	}
}

func TestMailContactCrudSaveDuplicateEmailIsReadable(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	router := newMailContactCrudRouter(t, f.svc, nil)
	injectMailWords(t, map[string]map[string]string{
		"mail.err.contactEmailExists": {"zh-CN": "该邮箱已存在", "en-US": "already exists"},
	})
	if err := f.seedContact(ctx, "qa.taken@example.invalid", "占位", mailmodel.ContactStatusPending, ""); err != nil {
		t.Fatal(err)
	}

	code, body := mailCrudWrite(t, router, "/api/mail/contact/save", mailCrudForm(
		"id", "0", "email", "QA.Taken@Example.invalid", "name", "重复", "tags", "", "status", "",
	))
	if code != http.StatusConflict {
		t.Fatalf("重复邮箱是预期内的业务冲突，应 409，实际 %d（%s）", code, body)
	}
	mailCrudContains(t, body, "重复邮箱新建", "该邮箱已存在", "already exists")

	var n int64
	if err := f.db.Model(&mailmodel.MailContactEntity{}).
		Where("lower(email) = lower(?)", "qa.taken@example.invalid").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("重复邮箱不该落库：期望 1 行，实际 %d 行", n)
	}
}

func TestMailContactCrudEmailChangeRespectsSuppression(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)
	if err := m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
		Email: "qa.blocked@example.invalid", Reason: mailmodel.SuppressionReasonUnsubscribe,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.seedContact(ctx, "qa.mover@example.invalid", "迁移的人", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	row, err := m.GetContactByEmail(ctx, "qa.mover@example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	code, body := mailCrudWrite(t, router, "/api/mail/contact/save", mailCrudForm(
		"id", strconv.FormatUint(row.ID, 10), "email", "qa.blocked@example.invalid",
		"name", "迁移的人", "tags", "", "status", "",
	))
	if code != http.StatusOK {
		t.Fatalf("改邮箱撞抑制名单应成功（落到名单对应的终态），实际 %d（%s）", code, body)
	}
	moved, err := m.GetContactByEmail(ctx, "qa.blocked@example.invalid")
	if err != nil {
		t.Fatalf("新邮箱应已落到该行：%v", err)
	}
	if moved.ID != row.ID {
		t.Fatalf("应改同一行：期望 id=%d，实际 %d", row.ID, moved.ID)
	}
	if moved.Status != mailmodel.ContactStatusUnsubscribed {
		t.Fatalf("新邮箱在抑制名单里，状态应为 unsubscribed，实际 %q", moved.Status)
	}
	if _, err := m.GetContactByEmail(ctx, "qa.mover@example.invalid"); err == nil {
		t.Fatal("旧邮箱不该还留着这一行")
	}
	sup, err := m.FindSuppressionByEmailTx(ctx, nil, "qa.blocked@example.invalid")
	if err != nil || sup.Reason != mailmodel.SuppressionReasonUnsubscribe {
		t.Fatalf("抑制记录被改动：%v / %+v", err, sup)
	}
}

func TestMailContactCrudDeleteKeepsSuppression(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)
	if err := m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
		Email: "qa.keep@example.invalid", Reason: mailmodel.SuppressionReasonComplaint,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.seedContact(ctx, "qa.keep@example.invalid", "待删", mailmodel.ContactStatusUnsubscribed, ""); err != nil {
		t.Fatal(err)
	}
	row, err := m.GetContactByEmail(ctx, "qa.keep@example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	code, body := mailCrudWrite(t, router, "/api/mail/contact/delete", mailCrudForm("id", strconv.FormatUint(row.ID, 10)))
	if code != http.StatusOK {
		t.Fatalf("删除联系人应 200，实际 %d（%s）", code, body)
	}
	if _, err := m.GetContactByEmail(ctx, "qa.keep@example.invalid"); err == nil {
		t.Fatal("联系人应已删除")
	}
	sup, err := m.FindSuppressionByEmailTx(ctx, nil, "qa.keep@example.invalid")
	if err != nil {
		t.Fatalf("删除联系人时把抑制记录也删了：%v", err)
	}
	if sup.Reason != mailmodel.SuppressionReasonComplaint {
		t.Fatalf("抑制记录被改写：%q", sup.Reason)
	}
}

func TestMailContactCrudDeleteWithoutIDIsReadable(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newMailContactCrudRouter(t, f.svc, nil)

	for _, form := range []url.Values{{"id": {"0"}}, {}} {
		code, body := mailCrudWrite(t, router, "/api/mail/contact/delete", form)
		if code != http.StatusBadRequest {
			t.Fatalf("删除缺形状应 400，实际 %d（%s）", code, body)
		}
		mailCrudContains(t, body, "删除缺形状", "参数", "invalid")
	}
}

func TestMailContactCrudDeleteMissingRowIsNotReportedAsSuccess(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)
	injectMailWords(t, map[string]map[string]string{
		"mail.err.contactNotFound": {"zh-CN": "联系人不存在", "en-US": "not found"},
	})

	var maxID uint64
	if err := f.db.Model(&mailmodel.MailContactEntity{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		t.Fatal(err)
	}

	code, body := mailCrudWrite(t, router, "/api/mail/contact/delete", mailCrudForm("id", strconv.FormatUint(maxID+999999, 10)))
	if code != http.StatusNotFound {
		t.Fatalf("删不存在的 id 应 404（不许报「删除成功」），实际 %d（%s）", code, body)
	}
	mailCrudContains(t, body, "单条删除不存在", "联系人不存在", "not found")

	if err := f.seedContact(ctx, "qa.single.delete@example.invalid", "单删", mailmodel.ContactStatusPending, ""); err != nil {
		t.Fatal(err)
	}
	row, err := m.GetContactByEmail(ctx, "qa.single.delete@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	code, body = mailCrudWrite(t, router, "/api/mail/contact/delete", mailCrudForm("id", strconv.FormatUint(row.ID, 10)))
	if code != http.StatusOK {
		t.Fatalf("删真实存在的联系人应 200，实际 %d（%s）", code, body)
	}
	if _, err := m.GetContactByEmail(ctx, "qa.single.delete@example.invalid"); err == nil {
		t.Fatal("联系人应已删除")
	}
}

func TestMailContactCrudStatusMissingIDIsNotReportedAsSuccess(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, mailCrudPagePerms())

	var maxID uint64
	if err := f.db.Model(&mailmodel.MailContactEntity{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		t.Fatal(err)
	}
	missing := maxID + 999999

	countOf := func(table string) int64 {
		var n int64
		if err := f.db.Table(table).Count(&n).Error; err != nil {
			t.Fatalf("统计 %s 失败：%v", table, err)
		}
		return n
	}
	contactsBefore, supBefore := countOf("mail_contacts"), countOf("mail_suppressions")

	injectMailWords(t, map[string]map[string]string{
		"mail.err.contactNotFound": {"zh-CN": "联系人不存在", "en-US": "not found"},
	})

	rec := mailCrudPagePost(t, router, "/admin/mail/contact/status", url.Values{
		"id": {strconv.FormatUint(missing, 10)}, "status": {mailmodel.ContactStatusSubscribed},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在的 id 必须走失败片段并给 404（不能报成功），实际 %d（%s）", rec.Code, firstN(rec.Body.String(), 300))
	}
	page := rec.Body.String()
	if !strings.Contains(page, `data-ui-error`) || !strings.Contains(page, "联系人不存在") {
		t.Fatalf("不存在的 id 必须给失败事实，报成功就是运营看到「已更新」而谁都没动：%s", firstN(page, 400))
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("写响应应带 no-store，得到 %q", cc)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("写响应是响应体而不是 302，不该有 Location：%q", loc)
	}

	recBulk := mailCrudPagePost(t, router, "/admin/mail/contacts/bulk-status", url.Values{
		"ids": {strconv.FormatUint(missing, 10)}, "status": {mailmodel.ContactStatusUnsubscribed},
	})
	if recBulk.Code != http.StatusNotFound {
		t.Fatalf("整批都不存在必须失败并给 404（不能报 0 条成功），实际 %d（%s）", recBulk.Code, firstN(recBulk.Body.String(), 300))
	}
	if !strings.Contains(recBulk.Body.String(), "data-ui-error") {
		t.Fatalf("整批拒绝应落在错误区：%s", firstN(recBulk.Body.String(), 400))
	}

	if got := countOf("mail_contacts"); got != contactsBefore {
		t.Fatalf("不存在的 id 不该动联系人表：%d → %d", contactsBefore, got)
	}
	if got := countOf("mail_suppressions"); got != supBefore {
		t.Fatalf("不存在的 id 不该碰抑制名单：%d → %d", supBefore, got)
	}
	var rows int64
	if err := f.db.Table("mail_contacts").Where("id = ?", missing).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("不存在的 id 不该凭空出现一行：id=%d 行数=%d", missing, rows)
	}

	if err := f.seedContact(ctx, "qa.status.missing@example.invalid", "状态探针",
		mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	row, err := m.GetContactByEmail(ctx, "qa.status.missing@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	rec = mailCrudPagePost(t, router, "/admin/mail/contact/status", url.Values{
		"id": {strconv.FormatUint(row.ID, 10)}, "status": {mailmodel.ContactStatusUnsubscribed},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("真实 id 改状态应 200，实际 %d：%s", rec.Code, firstN(rec.Body.String(), 400))
	}
	if got := countOf("mail_suppressions"); got != supBefore+1 {
		t.Fatalf("退订必须写抑制名单（正例的副作用证据）：%d → %d", supBefore, got)
	}
	rec = mailCrudPagePost(t, router, "/admin/mail/contact/status", url.Values{
		"id": {strconv.FormatUint(row.ID, 10)}, "status": {mailmodel.ContactStatusSubscribed},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("真实 id 必须返回成功片段：%d %s", rec.Code, firstN(rec.Body.String(), 400))
	}
	after, err := m.GetContact(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != mailmodel.ContactStatusSubscribed {
		t.Fatalf("页面通道应把状态改回 subscribed，实际 %q", after.Status)
	}
}

func TestMailContactCrudTemplateShapedPayloads(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)

	code, body := mailCrudWrite(t, router, "/api/mail/contact/save", mailCrudForm(
		"id", "0", "email", "qa.tplshape@example.invalid", "name", "模板形状",
		"tags", "vip", "status", string(mailmodel.ContactStatusPending), "consent_source", "",
	))
	if code != http.StatusOK {
		t.Fatalf("新建应 200，实际 %d（%s）", code, body)
	}
	row, err := m.GetContactByEmail(ctx, "qa.tplshape@example.invalid")
	if err != nil {
		t.Fatalf("新建后查不到：%v", err)
	}

	code, body = mailCrudWrite(t, router, "/api/mail/contact/save", mailCrudForm(
		"id", strconv.FormatUint(row.ID, 10), "email", "qa.tplshape@example.invalid", "name", "模板形状改名",
		"tags", "vip, 华南", "status", "", "consent_source", "",
	))
	if code != http.StatusOK {
		t.Fatalf("编辑应 200，实际 %d（%s）", code, body)
	}
	updated, err := m.GetContactByEmail(ctx, "qa.tplshape@example.invalid")
	if err != nil {
		t.Fatalf("编辑后查不到：%v", err)
	}
	if updated.ID != row.ID || updated.Name == nil || *updated.Name != "模板形状改名" {
		t.Fatalf("编辑没落到同一行或姓名未更新：id=%d name=%v", updated.ID, updated.Name)
	}

	code, body = mailCrudWrite(t, router, "/api/mail/contact/delete", mailCrudForm("id", strconv.FormatUint(row.ID, 10)))
	if code != http.StatusOK {
		t.Fatalf("单删应 200，实际 %d（%s）", code, body)
	}
	if _, err := m.GetContactByEmail(ctx, "qa.tplshape@example.invalid"); err == nil {
		t.Fatal("单删后联系人应已删除")
	}

	if code, body := mailCrudWrite(t, router, "/api/mail/contact/save", mailCrudForm(
		"id", "0", "email", "qa.tplshape.keep@example.invalid", "name", "对照组",
		"tags", "", "status", string(mailmodel.ContactStatusPending), "consent_source", "",
	)); code != http.StatusOK {
		t.Fatalf("准备对照联系人失败：%d（%s）", code, body)
	}
	if _, err := m.GetContactByEmail(ctx, "qa.tplshape.keep@example.invalid"); err != nil {
		t.Fatalf("对照组联系人落库失败：%v", err)
	}
	for _, form := range []url.Values{
		{"id": {"abc"}, "email": {"qa.tplshape.keep@example.invalid"}},
		{"id": {"-1"}},
	} {
		code, body := mailCrudWrite(t, router, "/api/mail/contact/save", form)
		if code != http.StatusBadRequest {
			t.Fatalf("非法 id 的保存应 400，实际 %d（%s）", code, body)
		}
		mailCrudContains(t, body, "非法 id", "参数", "invalid")
	}
	if _, err := m.GetContactByEmail(ctx, "qa.tplshape.keep@example.invalid"); err != nil {
		t.Fatalf("被拒的请求不该动数据，但对照联系人没了：%v", err)
	}
}

func TestMailContactCrudBulkDelete(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)
	for _, email := range []string{"qa.bulk1@example.invalid", "qa.bulk2@example.invalid", "qa.bulk3@example.invalid"} {
		if err := f.seedContact(ctx, email, "批量", mailmodel.ContactStatusPending, ""); err != nil {
			t.Fatal(err)
		}
	}
	drop1, err := m.GetContactByEmail(ctx, "qa.bulk1@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	drop2, err := m.GetContactByEmail(ctx, "qa.bulk2@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	keep, err := m.GetContactByEmail(ctx, "qa.bulk3@example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	var maxID uint64
	if err := f.db.Model(&mailmodel.MailContactEntity{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		t.Fatal(err)
	}
	missing := strconv.FormatUint(maxID+999999, 10)

	// 纯合法批次：一次事务全部删除，只播报公共成功。
	code, body := mailCrudWrite(t, router, "/api/mail/contact/delete", url.Values{"ids": {
		strconv.FormatUint(drop1.ID, 10),
		strconv.FormatUint(drop2.ID, 10),
	}})
	if code != http.StatusOK || !strings.Contains(body, `role="status"`) {
		t.Fatalf("合法批量删除应 200 公共提示，实际 %d（%s）", code, firstN(body, 400))
	}

	// 混合合法 / 非法 / 不存在：整批拒绝，已存在的那条也不能被删。
	if err := f.seedContact(ctx, "qa.bulk.mixed@example.invalid", "混合", mailmodel.ContactStatusPending, ""); err != nil {
		t.Fatal(err)
	}
	mixed, err := m.GetContactByEmail(ctx, "qa.bulk.mixed@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	code, body = mailCrudWrite(t, router, "/api/mail/contact/delete", url.Values{"ids": {
		strconv.FormatUint(mixed.ID, 10),
		missing,
		"qa-not-a-number",
	}})
	if code != http.StatusBadRequest || !strings.Contains(body, "data-ui-error") {
		t.Fatalf("混合批次应整批 400，实际 %d（%s）", code, firstN(body, 400))
	}
	if strings.Contains(body, missing) || strings.Contains(body, "qa-not-a-number") {
		t.Fatalf("失败区不逐条列明细（明细只进 data.items）：%s", firstN(body, 600))
	}
	if _, err := m.GetContactByEmail(ctx, "qa.bulk.mixed@example.invalid"); err != nil {
		t.Fatalf("整批回滚后合法行不该被删：%v", err)
	}
	if _, err := m.GetContactByEmail(ctx, "qa.bulk1@example.invalid"); err == nil {
		t.Fatal("qa.bulk1 应已删除")
	}
	if _, err := m.GetContactByEmail(ctx, "qa.bulk2@example.invalid"); err == nil {
		t.Fatal("qa.bulk2 应已删除")
	}
	if _, err := m.GetContactByEmail(ctx, "qa.bulk3@example.invalid"); err != nil {
		t.Fatalf("没点选的 qa.bulk3（id=%d）不该被删：%v", keep.ID, err)
	}
}

func TestMailContactCrudBulkTag(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)
	if err := f.seedContact(ctx, "qa.tag1@example.invalid", "标签一", mailmodel.ContactStatusPending, "vip"); err != nil {
		t.Fatal(err)
	}
	if err := f.seedContact(ctx, "qa.tag2@example.invalid", "标签二", mailmodel.ContactStatusPending, "VIP"); err != nil {
		t.Fatal(err)
	}
	row1, err := m.GetContactByEmail(ctx, "qa.tag1@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	row2, err := m.GetContactByEmail(ctx, "qa.tag2@example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	code, body := mailCrudWrite(t, router, "/api/mail/contact/tag", url.Values{
		"ids":    {strconv.FormatUint(row1.ID, 10), strconv.FormatUint(row2.ID, 10)},
		"add":    {"VIP2, 华南"},
		"remove": {"vip"},
	})
	if code != http.StatusOK {
		t.Fatalf("批量打标签应 200，实际 %d（%s）", code, body)
	}
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("批量打标签成功应走公共提示，实际：%s", firstN(body, 400))
	}
	if strings.Contains(body, "已删除") {
		t.Fatalf("打标签的摘要不得用删除口径（联系人还在，说「已删除」是假话）：%s", firstN(body, 400))
	}

	got1, err := m.GetContactByEmail(ctx, "qa.tag1@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if len(got1.Tags) != 2 || got1.Tags[0] != "VIP2" || got1.Tags[1] != "华南" {
		t.Fatalf("vip 应被去掉并把新标签追加在后：%v", []string(got1.Tags))
	}
	got2, err := m.GetContactByEmail(ctx, "qa.tag2@example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	if len(got2.Tags) != 3 || got2.Tags[0] != "VIP" || got2.Tags[1] != "VIP2" || got2.Tags[2] != "华南" {
		t.Fatalf("大小写不同的标签不该被去掉：%v", []string(got2.Tags))
	}
}

func TestMailContactCrudBulkTagEmptyDeltaIsReadable(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)
	injectMailWords(t, map[string]map[string]string{
		"mail.err.contactTagEmpty": {"zh-CN": "请至少填一个标签", "en-US": "tag required"},
	})
	if err := f.seedContact(ctx, "qa.tagempty@example.invalid", "标签空", mailmodel.ContactStatusPending, "vip"); err != nil {
		t.Fatal(err)
	}
	row, err := m.GetContactByEmail(ctx, "qa.tagempty@example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	code, body := mailCrudWrite(t, router, "/api/mail/contact/tag", url.Values{
		"ids": {strconv.FormatUint(row.ID, 10)}, "add": {""}, "remove": {""},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("空差量应 400，实际 %d（%s）", code, body)
	}
	mailCrudContains(t, body, "空标签批量打标签", "请至少填一个标签", "tag required")

	got, err := m.GetContactByEmail(ctx, "qa.tagempty@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "vip" {
		t.Fatalf("拒绝的请求不该改标签：%v", []string(got.Tags))
	}
}

func mailCrudPagePerms() map[string]bool {
	return map[string]bool{
		"mail:contact_list":   true,
		"mail:contact_import": true,
		"mail:contact_status": true,
		"mail:contact_save":   true,
		"mail:contact_delete": true,
		"mail:contact_tag":    true,
		"mail:campaign_save":  true,
	}
}

func TestMailContactCrudPageRendersEntryPoints(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	router := newMailContactCrudRouter(t, f.svc, mailCrudPagePerms())
	if err := f.seedContact(ctx, "qa.render@example.invalid", "页面", mailmodel.ContactStatusSubscribed, "vip"); err != nil {
		t.Fatal(err)
	}

	rec := mailCrudGet(t, router, "/admin/mail/contacts")
	if rec.Code != http.StatusOK {
		t.Fatalf("页面状态码 %d，正文前 300 字：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-drawer-open="#tpl-contact-save"`,
		`id="tpl-contact-save"`,
		`id="tpl-contact-save-`,
		`hx-post="/api/mail/contact/save"`,
		`hx-post="/api/mail/contact/delete"`,
		`hx-post="/api/mail/contact/tag"`,
		`data-write-status`,
		`/admin/mail/contacts/bulk-status`,
		`id="mk-tag-options"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；正文长度 %d，前 600 字：\n%s", want, len(body), firstN(body, 600))
		}
	}
	for _, gone := range []string{
		"action=\"/admin/mail/contact/save\"",
		"action=\"/admin/mail/contact/delete\"",
		"formaction=\"/admin/mail/contacts/bulk-tag\"",
		"formaction=\"/admin/mail/contacts/bulk-delete\"",
		"data-write-url",
		`action="/admin/mail/contacts/bulk-status"`,
	} {
		if strings.Contains(body, gone) {
			t.Fatalf("页面仍有旧写通道 %q（本批的交付物就是把它删掉）", gone)
		}
	}
	if !strings.Contains(body, "vip") {
		t.Fatal("筛选区的标签候选应含库里已有的 vip")
	}
}

func TestMailContactCrudPageEmptyStateOffersBothEntries(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newMailContactCrudRouter(t, f.svc, mailCrudPagePerms())

	rec := mailCrudGet(t, router, "/admin/mail/contacts")
	if rec.Code != http.StatusOK {
		t.Fatalf("空库页面状态码 %d，正文前 300 字：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "新建一位联系人手工加一条") {
		t.Fatalf("空态文案未同时指向新建与导入；前 600 字：\n%s", firstN(body, 600))
	}
	if !strings.Contains(body, `href="/admin/mail/contacts/import-sheet"`) &&
		!strings.Contains(body, `data-drawer-open="#tpl-contact-import"`) {
		t.Fatal("空态应给出导入入口")
	}
	if !strings.Contains(body, `data-drawer-open="#tpl-contact-save"`) {
		t.Fatal("空态应给出新建入口")
	}
	if !strings.Contains(body, `id="tpl-contact-save"`) {
		t.Fatal("空态下新建抽屉本体也应渲染（否则入口点了没反应）")
	}
}

func buttonsOf(perms map[string]bool) map[string]bool {
	out := make(map[string]bool, len(perms))
	for code := range perms {
		out[strings.ReplaceAll(code, ":", ".")] = true
	}
	return out
}
