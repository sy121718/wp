package feature

// mail_contact_crud_test.go — 联系人 CRUD + 标签管理的接口链路（task-4）。
//
// 从 handler 入口打进去，断言三件事：
//   · PRG 回跳（失败回 ?err=、成功回 ?ok=1，且落点始终是 /admin/mail/contacts）；
//   · 落库结果（邮箱归一化、标签精确匹配、状态终态）；
//   · 对外文案受控 —— 重复邮箱这种可预期的失败不能把 23505 原文摆到运营面前。
//
// 另一条合规红线单独钉住：**删联系人绝不动 mail_suppressions**。
// 顺手删掉抑制记录，下次导入就会把退订者复活（service.DeleteContacts 的注释）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
)

// mailCrudLeakTokens 回执里不该出现的东西：驱动名、SQL 状态码、表名、语句片段。
var mailCrudLeakTokens = []string{
	"23505", "duplicate key", "pgx", "mail_contacts", "mail_suppressions", "ERROR:", "SELECT ",
}

// newMailContactCrudRouter 只挂这一组页面的最小 router（与 newMailPageFixture 同形）。
//
// perms 非 nil 时注入 PermSet：页面按权限渲染按钮，不给 PermSet 就什么都看不到。
func newMailContactCrudRouter(t *testing.T, svc *mailservice.Service, perms map[string]bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("gin 渲染错误: %v", e.Err)
		}
	})
	if perms != nil {
		router.Use(func(c *gin.Context) {
			c.Set(shell.PermSetKey, perms)
			c.Set(shell.ButtonsKey, buttonsOf(perms))
			c.Next()
		})
	}
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := mailhttp.NewMailPageHandle(svc)
	router.GET("/admin/mail/contacts", h.MailContactsPage)
	router.POST("/admin/mail/contact/save", h.MailContactSave)
	router.POST("/admin/mail/contact/delete", h.MailContactDelete)
	router.POST("/admin/mail/contacts/bulk-delete", h.MailContactsBulkDelete)
	router.POST("/admin/mail/contacts/bulk-tag", h.MailContactsBulkTag)
	return router
}

func mailCrudPost(t *testing.T, router *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func mailCrudGet(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// mailCrudAssertOK 断言成功提示页，并校验回跳落点是联系人列表。
//
// PRG 的对外契约已从「302 + Location 上的 ok=1」换成「200 + 提示页」：结论文案在响应体里，
// 回跳地址在 meta refresh / 链接上（见 internal/module/mail/inbound/http/mail_jump.go）。
func mailCrudAssertOK(t *testing.T, rec *httptest.ResponseRecorder, label string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s：期望提示页 200，实际 %d，正文前 300 字：%s", label, rec.Code, firstN(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="ok"`) {
		t.Fatalf("%s：期望成功提示页；正文前 600 字：\n%s", label, firstN(body, 600))
	}
	if back := mailJumpBack(t, rec); !strings.HasPrefix(back, "/admin/mail/contacts") {
		t.Fatalf("%s：成功提示应回联系人列表，实际回跳 %q", label, back)
	}
}

// mailCrudFailText 断言失败提示页，返回正文（并断言没有内部细节漏出去）。
func mailCrudFailText(t *testing.T, rec *httptest.ResponseRecorder, label string) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s：期望提示页 200，实际 %d，正文前 300 字：%s", label, rec.Code, firstN(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("%s：期望失败提示页；正文前 600 字：\n%s", label, firstN(body, 600))
	}
	for _, token := range mailCrudLeakTokens {
		if strings.Contains(body, token) {
			t.Fatalf("%s：提示页泄漏了内部细节 %q", label, token)
		}
	}
	return body
}

// mailCrudFacingContains 回执必须命中候选之一（i18n 未初始化时回的是 key，有词条时是译文，
// 所以判据用子串而不是全等 —— 两种环境下都成立）。
func mailCrudFacingContains(t *testing.T, text, label string, want ...string) {
	t.Helper()
	for _, w := range want {
		if strings.Contains(text, w) {
			return
		}
	}
	t.Fatalf("%s：回执 %q 不含任何预期线索 %v", label, text, want)
}

// TestMailContactCrudSaveCreatesThenUpdates 新建（ID=0）与编辑（ID>0）走同一端点。
func TestMailContactCrudSaveCreatesThenUpdates(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)

	rec := mailCrudPost(t, router, "/admin/mail/contact/save", url.Values{
		"id": {"0"}, "email": {"QA.New@Example.invalid"}, "name": {"测试新人"},
		"tags": {"vip, 华南"}, "status": {"subscribed"}, "consent_source": {"官网表单勾选同意"},
	})
	mailCrudAssertOK(t, rec, "新建联系人")
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

	// 编辑：改邮箱 + 改标签 + 清空 name 以外的字段保持原值。
	rec = mailCrudPost(t, router, "/admin/mail/contact/save", url.Values{
		"id": {strconv.FormatUint(row.ID, 10)}, "email": {"qa.renamed@example.invalid"},
		"name": {"改名"}, "tags": {"vip"}, "status": {""}, "consent_source": {""},
	})
	mailCrudAssertOK(t, rec, "编辑联系人")
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

// TestMailContactCrudSaveDuplicateEmailIsReadable 重复邮箱是可预期失败：回可读文案、不落库、不 500。
func TestMailContactCrudSaveDuplicateEmailIsReadable(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	router := newMailContactCrudRouter(t, f.svc, nil)
	if err := f.seedContact(ctx, "qa.taken@example.invalid", "占位", mailmodel.ContactStatusPending, ""); err != nil {
		t.Fatal(err)
	}

	rec := mailCrudPost(t, router, "/admin/mail/contact/save", url.Values{
		"id": {"0"}, "email": {"QA.Taken@Example.invalid"}, "name": {"重复"}, "tags": {""}, "status": {""},
	})
	mailCrudFacingContains(t, mailCrudFailText(t, rec, "重复邮箱新建"), "重复邮箱新建",
		"contactEmailExists", "该邮箱已存在")

	var n int64
	if err := f.db.Model(&mailmodel.MailContactEntity{}).
		Where("lower(email) = lower(?)", "qa.taken@example.invalid").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("重复邮箱不该落库：期望 1 行，实际 %d 行", n)
	}
}

// TestMailContactCrudEmailChangeRespectsSuppression 改邮箱撞上抑制名单 → 落到名单原因对应的终态。
//
// 不搬抑制记录是刻意的（搬了等于让退订者换个地址继续收），所以旧地址的记录原样保留。
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

	rec := mailCrudPost(t, router, "/admin/mail/contact/save", url.Values{
		"id": {strconv.FormatUint(row.ID, 10)}, "email": {"qa.blocked@example.invalid"},
		"name": {"迁移的人"}, "tags": {""}, "status": {""},
	})
	mailCrudAssertOK(t, rec, "改邮箱撞抑制名单")
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

// TestMailContactCrudDeleteKeepsSuppression 删联系人只删联系人行。
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

	rec := mailCrudPost(t, router, "/admin/mail/contact/delete", url.Values{
		"id": {strconv.FormatUint(row.ID, 10)},
	})
	mailCrudAssertOK(t, rec, "删除联系人")
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

// TestMailContactCrudDeleteWithoutIDIsReadable 单条删除缺 id 时给可读回执。
func TestMailContactCrudDeleteWithoutIDIsReadable(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newMailContactCrudRouter(t, f.svc, nil)

	rec := mailCrudPost(t, router, "/admin/mail/contact/delete", url.Values{"id": {""}})
	mailCrudFailText(t, rec, "删除缺 id")
}

// TestMailContactCrudBulkDelete 批量删除：不存在的 id 计入跳过，其余照删。
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

	rec := mailCrudPost(t, router, "/admin/mail/contacts/bulk-delete", url.Values{
		"ids": {
			strconv.FormatUint(drop1.ID, 10),
			strconv.FormatUint(drop2.ID, 10),
			strconv.FormatUint(keep.ID+1000, 10), // 已不存在的 id：应计入跳过
			"qa-not-a-number",                    // 非法 id：也应计入跳过
		},
	})
	body := mailCrudFailText(t, rec, "批量删除")
	if !strings.Contains(body, "被跳过") {
		t.Fatalf("批量删除应带回执（已删除 / 跳过）；正文前 600 字：\n%s", firstN(body, 600))
	}
	if back := mailJumpBack(t, rec); !strings.HasPrefix(back, "/admin/mail/contacts") {
		t.Fatalf("批量删除提示应回列表页，实际回跳 %q", back)
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

// TestMailContactCrudBulkTag 批量打标签：加减同一次请求，精确匹配（大小写敏感）。
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

	rec := mailCrudPost(t, router, "/admin/mail/contacts/bulk-tag", url.Values{
		"ids":    {strconv.FormatUint(row1.ID, 10), strconv.FormatUint(row2.ID, 10)},
		"add":    {"VIP2, 华南"},
		"remove": {"vip"},
	})
	mailCrudAssertOK(t, rec, "批量打标签")

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
	// "VIP" 与 "vip" 不是同一个标签：精确匹配的口径下不能被去掉。
	if len(got2.Tags) != 3 || got2.Tags[0] != "VIP" || got2.Tags[1] != "VIP2" || got2.Tags[2] != "华南" {
		t.Fatalf("大小写不同的标签不该被去掉：%v", []string(got2.Tags))
	}
}

// TestMailContactCrudBulkTagEmptyDeltaIsReadable 加与减都空 → 可读拒绝，不动库。
func TestMailContactCrudBulkTagEmptyDeltaIsReadable(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)
	if err := f.seedContact(ctx, "qa.tagempty@example.invalid", "标签空", mailmodel.ContactStatusPending, "vip"); err != nil {
		t.Fatal(err)
	}
	row, err := m.GetContactByEmail(ctx, "qa.tagempty@example.invalid")
	if err != nil {
		t.Fatal(err)
	}

	rec := mailCrudPost(t, router, "/admin/mail/contacts/bulk-tag", url.Values{
		"ids": {strconv.FormatUint(row.ID, 10)}, "add": {""}, "remove": {""},
	})
	mailCrudFacingContains(t, mailCrudFailText(t, rec, "空标签批量打标签"), "空标签批量打标签",
		"contactTagEmpty", "标签")

	got, err := m.GetContactByEmail(ctx, "qa.tagempty@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "vip" {
		t.Fatalf("拒绝的请求不该改标签：%v", []string(got.Tags))
	}
}

// mailCrudPagePerms 页面渲染用的权限集合（与 marketingPagePerms 同族，含本批三条新权限）。
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

// TestMailContactCrudPageRendersEntryPoints 有权限时四个入口（新建 / 编辑 / 删除 / 批量打标签）都在页面上。
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
		`data-drawer-open="#tpl-contact-save"`,          // 页头「新建联系人」
		`id="tpl-contact-save"`,                         // 新建抽屉本体
		`id="tpl-contact-save-`,                         // 行内编辑抽屉
		`action="/admin/mail/contact/delete"`,           // 行内删除表单
		`formaction="/admin/mail/contacts/bulk-tag"`,    // 批量打标签按钮
		`formaction="/admin/mail/contacts/bulk-delete"`, // 批量删除按钮
		`id="mk-tag-options"`,                           // 标签候选 datalist
		`/admin/mail/contacts/bulk-status`,              // 原有批量改状态仍在
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；正文长度 %d，前 600 字：\n%s", want, len(body), firstN(body, 600))
		}
	}
	if !strings.Contains(body, "vip") {
		t.Fatal("筛选区的标签候选应含库里已有的 vip")
	}
}

// TestMailContactCrudPageEmptyStateOffersBothEntries 空态要同时指出「新建」与「导入」两条路。
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
}

// TestMailContactCrudPageHidesEntryPointsWithoutPermission 没权限就不渲染入口（页面自身不越权渲染）。
func TestMailContactCrudPageHidesEntryPointsWithoutPermission(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newMailContactCrudRouter(t, f.svc, map[string]bool{"mail:contact_list": true})

	rec := mailCrudGet(t, router, "/admin/mail/contacts")
	if rec.Code != http.StatusOK {
		t.Fatalf("页面状态码 %d，正文前 300 字：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	for _, banned := range []string{
		`data-drawer-open="#tpl-contact-save"`,
		`action="/admin/mail/contact/delete"`,
		`formaction="/admin/mail/contacts/bulk-tag"`,
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("没有对应权限却渲染了 %q", banned)
		}
	}
}

// TestMailContactCrudSubscribedRequiresConsentSource 置为「已订阅」必须写清同意来源。
//
// 这条规则页面文案一直在承诺（页头 help 的「不是走形式，而是留痕」、抽屉里的状态说明），
// 而导入入口是靠 ConsentDeclared 勾选强制的 —— 新建 / 编辑两条入口必须由服务端强制，
// 否则同一份名单从不同入口进来，合规要求宽严不一（文案在撒谎）。
//
// 判据是**最终状态**：改邮箱撞抑制名单时状态会被降级成终态，那种情况不该要求来源。
func TestMailContactCrudSubscribedRequiresConsentSource(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)

	t.Run("新建/无来源→拒绝且不落库", func(t *testing.T) {
		rec := mailCrudPost(t, router, "/admin/mail/contact/save", url.Values{
			"id": {"0"}, "email": {"qa.consent-new@example.invalid"}, "name": {"缺来源"},
			"tags": {""}, "status": {"subscribed"}, "consent_source": {""},
		})
		mailCrudFacingContains(t, mailCrudFailText(t, rec, "新建 subscribed 缺来源"), "新建 subscribed 缺来源",
			"consentSourceRequired", "同意来源")
		if _, err := m.GetContactByEmail(ctx, "qa.consent-new@example.invalid"); err == nil {
			t.Fatal("被拒绝的新建不该落库")
		}
	})

	t.Run("编辑改为已订阅/无来源→拒绝且状态不变", func(t *testing.T) {
		if err := f.seedContact(ctx, "qa.consent-edit@example.invalid", "待确认", mailmodel.ContactStatusPending, ""); err != nil {
			t.Fatal(err)
		}
		row, err := m.GetContactByEmail(ctx, "qa.consent-edit@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		rec := mailCrudPost(t, router, "/admin/mail/contact/save", url.Values{
			"id": {strconv.FormatUint(row.ID, 10)}, "email": {row.Email}, "name": {"待确认"},
			"tags": {""}, "status": {"subscribed"}, "consent_source": {""},
		})
		mailCrudFacingContains(t, mailCrudFailText(t, rec, "编辑改订阅缺来源"), "编辑改订阅缺来源",
			"consentSourceRequired", "同意来源")
		after, err := m.GetContactByEmail(ctx, "qa.consent-edit@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		if after.Status != mailmodel.ContactStatusPending {
			t.Fatalf("被拒绝的编辑不该改状态，实际 %q", after.Status)
		}
	})

	t.Run("编辑改为已订阅/带来源→成功且来源落库", func(t *testing.T) {
		row, err := m.GetContactByEmail(ctx, "qa.consent-edit@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		rec := mailCrudPost(t, router, "/admin/mail/contact/save", url.Values{
			"id": {strconv.FormatUint(row.ID, 10)}, "email": {row.Email}, "name": {"待确认"},
			"tags": {""}, "status": {"subscribed"}, "consent_source": {"2026-10 线下展会，本人书面同意"},
		})
		mailCrudAssertOK(t, rec, "编辑改订阅带来源")
		after, err := m.GetContactByEmail(ctx, "qa.consent-edit@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		if after.Status != mailmodel.ContactStatusSubscribed {
			t.Fatalf("状态应为 subscribed，实际 %q", after.Status)
		}
		if after.SubscribedAt == nil {
			t.Fatal("订阅态必须写 subscribed_at")
		}
		if after.ConsentSource == nil || !strings.Contains(*after.ConsentSource, "展会") {
			t.Fatalf("同意来源没落库：%v", after.ConsentSource)
		}
	})

	t.Run("最终状态被抑制名单降级→不要求来源", func(t *testing.T) {
		if err := f.seedContact(ctx, "qa.consent-move@example.invalid", "迁移", mailmodel.ContactStatusPending, ""); err != nil {
			t.Fatal(err)
		}
		if err := m.AddSuppression(ctx, &mailmodel.MailSuppressionEntity{
			Email: "qa.consent-blocked@example.invalid", Reason: mailmodel.SuppressionReasonUnsubscribe,
		}); err != nil {
			t.Fatal(err)
		}
		row, err := m.GetContactByEmail(ctx, "qa.consent-move@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		rec := mailCrudPost(t, router, "/admin/mail/contact/save", url.Values{
			"id": {strconv.FormatUint(row.ID, 10)}, "email": {"qa.consent-blocked@example.invalid"},
			"name": {"迁移"}, "tags": {""}, "status": {"subscribed"}, "consent_source": {""},
		})
		mailCrudAssertOK(t, rec, "改邮箱撞抑制名单")
		after, err := m.GetContactByEmail(ctx, "qa.consent-blocked@example.invalid")
		if err != nil {
			t.Fatal(err)
		}
		if after.Status != mailmodel.ContactStatusUnsubscribed {
			t.Fatalf("抑制名单里的新邮箱应落到 unsubscribed，实际 %q", after.Status)
		}
	})
}

// buttonsOf 把「权限码集合」转成「按钮码集合」（按钮码 = 权限码的 slug 形式，见迁移 589）。
// 测试直接给权限码更贴近业务语义；模板读的是按钮码，所以两条都注入。
func buttonsOf(perms map[string]bool) map[string]bool {
	out := make(map[string]bool, len(perms))
	for code := range perms {
		out[strings.ReplaceAll(code, ":", ".")] = true
	}
	return out
}
