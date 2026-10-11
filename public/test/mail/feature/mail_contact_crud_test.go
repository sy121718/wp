package feature

// mail_contact_crud_test.go — 联系人 CRUD + 标签管理的接口链路（task-4 / task-18）。
//
// 从 handler 入口打进去，断言三件事：
//   · **JSON 契约**（task-18 起）：写动作由浏览器直调 POST /api/mail/contact/{save,delete,tag}，
//     回 {code,message,data}；成功 / 失败都在响应体里，不再有 PRG 提示页（页面 POST 已下线）；
//   · 落库结果（邮箱归一化、标签精确匹配、状态终态）；
//   · 对外文案受控 —— 重复邮箱这种可预期的失败不能把 23505 原文摆到运营面前。
//
// 另一条合规红线单独钉住：**删联系人绝不动 mail_suppressions**。
// 顺手删掉抑制记录，下次导入就会把退订者复活（service.DeleteContacts 的注释）。

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/internal/templates"
)

// mailCrudLeakTokens 回执里不该出现的东西：驱动名、SQL 状态码、表名、语句片段。
var mailCrudLeakTokens = []string{
	"23505", "duplicate key", "pgx", "mail_contacts", "mail_suppressions", "ERROR:", "SELECT ",
}

const mailCrudUserID = int64(987654311)

// newMailContactCrudRouter 只挂联系人这一页 + 三条 JSON 写端点（与页面注册同源）。
//
// 写完之后的通道全部是 JSON：页面上的表单/按钮带 data-write-api，浏览器直接打这三条端点。
// 页面 POST（/admin/mail/contact/{save,delete}、/admin/mail/contacts/bulk-{delete,tag}）已下线，
// 所以这里挂不上它们 —— 想挂也没有 handler 了。
//
// 状态写是**两条通道都在**的例外：JSON 端点 /api/mail/contact/status 与页面 POST
// /admin/mail/contact/status（抽屉表单仍走它，见 mail_page.go 的 MailContactStatus），
// 两条都挂上，才能在同一个用例里比对同一件事的两种回执。
func newMailContactCrudRouter(t *testing.T, svc *mailservice.Service, perms map[string]bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", mailCrudUserID) // builtin.GetUserID 的入口：操作人取自会话，不取自请求体
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
	page := mailhttp.NewMailPageHandle(svc)
	router.GET("/admin/mail/contacts", page.MailContactsPage)
	// JSON 写端点住在 *Handle 上（页面 handler 在 mailPageHandle 上，两者不是一个类型）——
	// 与生产装配同形：页面在 /admin 组，写在 /api/mail 组。
	api := router.Group("/api/mail")
	write := mailhttp.NewHandle(svc)
	api.POST("/contact/save", write.ContactSave)
	api.POST("/contact/delete", write.ContactDelete)
	api.POST("/contact/tag", write.ContactTag)
	api.POST("/contact/status", write.ContactStatus)
	router.POST("/admin/mail/contact/status", page.MailContactStatus)
	// 批量改状态同样没有 JSON 端点（它要同步写抑制名单，语义与单条抽屉一致），
	// 页面表单调 /admin/mail/contacts/bulk-status —— 第三条通道挂上，才不会只有单条被测。
	router.POST("/admin/mail/contacts/bulk-status", page.MailContactsBulkStatus)
	return router
}

// mailCrudPagePost 发一个页面表单 POST（提示页通道：真模板 + 真 handler）。
func mailCrudPagePost(t *testing.T, router *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// mailCrudWrite 发一个 JSON 写请求，返回状态码与解析后的响应体。
//
// 顺带守一条：**回执里不得出现内部细节**（驱动名 / SQL 状态码 / 表名 / 语句片段）——
// 这条以前是靠页面提示页的正文断言的，现在断言 JSON 正文（含 message）。
func mailCrudWrite(t *testing.T, router *gin.Engine, path string, payload interface{}) (int, map[string]interface{}) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("构造请求体失败：%v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := map[string]interface{}{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s：响应不是 JSON：%s", path, firstN(rec.Body.String(), 300))
		}
	}
	text := rec.Body.String()
	for _, token := range mailCrudLeakTokens {
		if strings.Contains(text, token) {
			t.Fatalf("%s：回执泄漏了内部细节 %q：%s", path, token, firstN(text, 300))
		}
	}
	return rec.Code, body
}

// mailCrudGet 取页面（HTML 渲染路径：真模板 + 真 handler）。
func mailCrudGet(t *testing.T, router *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// mailCrudData 取成功回执的 data 对象。
func mailCrudData(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("回执缺少 data 对象：%#v", body)
	}
	return data
}

// mailCrudMsg 取回执文案（i18n 未初始化时回的是 key，有词条时是译文 —— 判据用子串，
// 两种环境下都成立，见 mailCrudFacingContains）。
func mailCrudMsg(t *testing.T, body map[string]interface{}) string {
	t.Helper()
	msg, _ := body["message"].(string)
	return msg
}

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

	code, body := mailCrudWrite(t, router, "/api/mail/contact/save", map[string]interface{}{
		"id": 0, "email": "QA.New@Example.invalid", "name": "测试新人",
		"tags": "vip, 华南", "status": "subscribed", "consent_source": "官网表单勾选同意",
	})
	if code != http.StatusOK {
		t.Fatalf("新建联系人应 200，实际 %d（%#v）", code, body)
	}
	mailCrudFacingContains(t, mailCrudMsg(t, body), "新建联系人", "mail.msg.saveSuccess", "保存成功")
	if id, _ := mailCrudData(t, body)["id"].(float64); id == 0 {
		t.Fatalf("新建回执应带新 id（脚本 / AI 工具靠它拿到刚写入的身份）：%#v", body)
	}

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
	code, body = mailCrudWrite(t, router, "/api/mail/contact/save", map[string]interface{}{
		"id": row.ID, "email": "qa.renamed@example.invalid",
		"name": "改名", "tags": "vip", "status": "", "consent_source": "",
	})
	if code != http.StatusOK {
		t.Fatalf("编辑联系人应 200，实际 %d（%#v）", code, body)
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

	code, body := mailCrudWrite(t, router, "/api/mail/contact/save", map[string]interface{}{
		"id": 0, "email": "QA.Taken@Example.invalid", "name": "重复", "tags": "", "status": "",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("重复邮箱是预期内的业务失败，应 400，实际 %d（%#v）", code, body)
	}
	mailCrudFacingContains(t, mailCrudMsg(t, body), "重复邮箱新建", "contactEmailExists", "该邮箱已存在")

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

	code, body := mailCrudWrite(t, router, "/api/mail/contact/save", map[string]interface{}{
		"id": row.ID, "email": "qa.blocked@example.invalid", "name": "迁移的人", "tags": "", "status": "",
	})
	if code != http.StatusOK {
		t.Fatalf("改邮箱撞抑制名单应成功（落到名单对应的终态），实际 %d（%#v）", code, body)
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

	code, body := mailCrudWrite(t, router, "/api/mail/contact/delete", map[string]interface{}{"id": row.ID})
	if code != http.StatusOK {
		t.Fatalf("删除联系人应 200，实际 %d（%#v）", code, body)
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

// TestMailContactCrudDeleteWithoutIDIsReadable 单条删除缺 id / id=0 时给可读回执。
func TestMailContactCrudDeleteWithoutIDIsReadable(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newMailContactCrudRouter(t, f.svc, nil)

	// id=0 与「一个形状都不给」都必须是受控 400：这两种请求以前会走页面 POST 的
	// 「回 ?err= 的提示页」，现在它们连 handler 的业务分支都不进。
	for _, payload := range []map[string]interface{}{{"id": 0}, {}} {
		code, body := mailCrudWrite(t, router, "/api/mail/contact/delete", payload)
		if code != http.StatusBadRequest {
			t.Fatalf("删除缺形状应 400，实际 %d（%#v）", code, body)
		}
		mailCrudFacingContains(t, mailCrudMsg(t, body), "删除缺形状", "mail.err.invalidParam", "参数")
	}
}

// TestMailContactCrudDeleteMissingRowIsNotReportedAsSuccess 单条删除一个不存在的 id：报「不存在」而不是成功。
//
// 真库才答得出来的一问：service 返回的是 RowsAffected，0 行意味着这一条本来就不在。
// 若忽略它，回执就会说「删除成功」而实际什么都没删（运营据此以为列表已经干净了，
// 而那一行只是别人先删掉了）。与批量档是同一个事实口径，差别只在载体：
// 批量是逐条 skipped + 原因，单条没有「部分成功」这个 channel，所以整请求失败。
func TestMailContactCrudDeleteMissingRowIsNotReportedAsSuccess(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)

	// 与批量用例同款：本包其它用例会往同一张表塞上千行，偏移量猜不得。
	var maxID uint64
	if err := f.db.Model(&mailmodel.MailContactEntity{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		t.Fatal(err)
	}

	code, body := mailCrudWrite(t, router, "/api/mail/contact/delete", map[string]interface{}{"id": maxID + 999999})
	if code != http.StatusBadRequest {
		t.Fatalf("删不存在的 id 应 400（不许报「删除成功」），实际 %d（%#v）", code, body)
	}
	mailCrudFacingContains(t, mailCrudMsg(t, body), "单条删除不存在", "contactNotFound", "联系人不存在")

	// 对照面：同一个端点删真实存在的行仍 200，且行确实从库里消失。
	if err := f.seedContact(ctx, "qa.single.delete@example.invalid", "单删", mailmodel.ContactStatusPending, ""); err != nil {
		t.Fatal(err)
	}
	row, err := m.GetContactByEmail(ctx, "qa.single.delete@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	code, body = mailCrudWrite(t, router, "/api/mail/contact/delete", map[string]interface{}{"id": row.ID})
	if code != http.StatusOK {
		t.Fatalf("删真实存在的联系人应 200，实际 %d（%#v）", code, body)
	}
	if _, err := m.GetContactByEmail(ctx, "qa.single.delete@example.invalid"); err == nil {
		t.Fatal("联系人应已删除")
	}
}

// TestMailContactCrudStatusMissingIDIsNotReportedAsSuccess 状态写对不存在的 id 必须报失败：
// JSON 400（ErrContactNotFound）+ 页面 err 档（no-store、无 Location、不自动跳）+ 库里没有任何副作用。
//
// 为什么单列一条（2026-10-08）：路由层的回跳用例里那句注释一度写成「UpdateContactStatus 不查存在性，
// 0 行也回成功」，与 service 实际行为相反 —— service 先 GetContact
// （internal/module/mail/service/mail_contact.go:81-84），未命中回 ErrContactNotFound。
// 错注释能存活是因为**状态写这条路径上「不存在的 id」没有任何测试**（删除 / 打标签两条路径有，
// 见上面两条 DeleteMissingRow 用例）。这条把行为钉住，与同一批的注释修正配对。
//
// 两条通道都验：页面 POST 与 JSON 端点走的是同一个 service，但回执形态不同 —— 只验一条会漏掉
// 「其中一条把失败吞成成功」这种偏差（历史上页面通道与 JSON 通道分头演进过）。
func TestMailContactCrudStatusMissingIDIsNotReportedAsSuccess(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, mailCrudPagePerms())

	// 与批量用例同款：本包其它用例会往同一张表塞上千行，偏移量猜不得。
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

	// ① JSON 通道：400 + 可读文案（把「联系人不存在」翻不成也要能认出 key）。
	code, body := mailCrudWrite(t, router, "/api/mail/contact/status", map[string]interface{}{
		"id": missing, "status": mailmodel.ContactStatusSubscribed,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("状态写不存在的 id 应 400（不许报「已更新」），实际 %d（%#v）", code, body)
	}
	mailCrudFacingContains(t, mailCrudMsg(t, body), "状态写不存在", "contactNotFound", "联系人不存在")

	// ② 页面通道：200 整页提示，但必须是 err 档、不自动跳、无 Location（与成功档的可观察差别）。
	rec := mailCrudPagePost(t, router, "/admin/mail/contact/status", url.Values{
		"id": {strconv.FormatUint(missing, 10)}, "status": {mailmodel.ContactStatusSubscribed},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("页面状态写应回 200 提示页，实际 %d（%s）", rec.Code, firstN(rec.Body.String(), 300))
	}
	page := rec.Body.String()
	if !strings.Contains(page, `data-jump-state="err"`) {
		t.Fatalf("不存在的 id 必须走 err 档，报成功就是运营看到「已更新」而谁都没动：%s", firstN(page, 400))
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("提示页应带 no-store，得到 %q", cc)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("这条通道是响应体而不是 302，不该有 Location：%q", loc)
	}
	if strings.Contains(page, `http-equiv="refresh"`) {
		t.Fatal("err 档不该自动跳走（运营要先看清原因再走）")
	}

	// ②′ 批量通道（页面 POST /admin/mail/contacts/bulk-status）：整批只有这个不存在的 id ——
	// 它必须计成「跳过」而不是「更新」，整体走 err 档（只报成功数会让「选了 N 个」看起来全做完了）。
	recBulk := mailCrudPagePost(t, router, "/admin/mail/contacts/bulk-status", url.Values{
		"ids": {strconv.FormatUint(missing, 10)}, "status": {mailmodel.ContactStatusUnsubscribed},
	})
	if recBulk.Code != http.StatusOK {
		t.Fatalf("批量状态写应回 200 提示页，实际 %d（%s）", recBulk.Code, firstN(recBulk.Body.String(), 300))
	}
	bulkPage := recBulk.Body.String()
	if !strings.Contains(bulkPage, `data-jump-state="err"`) {
		t.Fatalf("整批都不存在时必须走 err 档：%s", firstN(bulkPage, 400))
	}
	if strings.Contains(bulkPage, `data-jump-state="ok"`) {
		t.Fatal("同一页面上不该同时出现成功档（说明计数把不存在的 id 算成了已更新）")
	}

	// ③ 数据不变：两条通道都没写库（状态列 / 抑制名单 / 凭空多行三样都查）。
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

	// ④ 正例自检（判据不是空转）：真实存在的 id 走同样两条通道 —— JSON 200、页面 ok 档，
	// 且退订那一步**真的写了抑制名单**（证明 ③ 的「计数不变」是「没写」而不是判据失灵）。
	if err := f.seedContact(ctx, "qa.status.missing@example.invalid", "状态探针",
		mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	row, err := m.GetContactByEmail(ctx, "qa.status.missing@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	code, body = mailCrudWrite(t, router, "/api/mail/contact/status", map[string]interface{}{
		"id": row.ID, "status": mailmodel.ContactStatusUnsubscribed,
	})
	if code != http.StatusOK {
		t.Fatalf("真实 id 改状态应 200，实际 %d（%#v）", code, body)
	}
	rec = mailCrudPagePost(t, router, "/admin/mail/contact/status", url.Values{
		"id": {strconv.FormatUint(row.ID, 10)}, "status": {mailmodel.ContactStatusSubscribed},
	})
	if !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("真实 id 必须走 ok 档：%s", firstN(rec.Body.String(), 400))
	}
	if got := countOf("mail_suppressions"); got != supBefore+1 {
		t.Fatalf("退订必须写抑制名单（正例的副作用证据）：%d → %d", supBefore, got)
	}
	after, err := m.GetContact(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != mailmodel.ContactStatusSubscribed {
		t.Fatalf("页面通道应把状态改回 subscribed，实际 %q", after.Status)
	}
}

// TestMailContactCrudTemplateShapedPayloads 页面上真实控件发出来的身体，端点必须收下。
//
// 为什么单独立一条（2026-10-08 的 P1）：模板里的隐藏 id 经 json_write 发出来是**字符串** "7"，
// 而本端点的严格形状拒绝字符串数字 → 页面上的新增 / 编辑 / 单删全部 400。Node 通用用例当时
// 全绿也发现不了它，因为那些用例的控件属性是人手写的。修法在模板侧：写表单显式声明
// `data-write-number="id"`（基座据此在发请求前把该字段转成数字，未声明的字段一律不动）。
//
// 这条测试钉的是**契约两端**，两条一起才闭合：
//
//	· 前三条 = 真模板会发出来的身体（数字 id + 字符串字段）→ 必须 200 且真的落库；
//	· 第四条 = 修复前那种身体（字符串 id）→ 必须 400：谁把模板上的声明删掉，这里立刻红。
//
// 模板侧「真实控件 → payload」由 internal/templates/static/js/ui/json_write.test.cjs 的
// 「真模板 → payload」用例覆盖（它读的就是同一个 internal/templates/admin/mail/mail_contacts.html），
// 两边用同一个形状。
func TestMailContactCrudTemplateShapedPayloads(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	m := mailmodel.NewMailModel(f.db)
	router := newMailContactCrudRouter(t, f.svc, nil)

	// ① 新建抽屉：id=0（数字）+ 表单具名字段（字符串）
	code, body := mailCrudWrite(t, router, "/api/mail/contact/save", map[string]interface{}{
		"id": 0, "email": "qa.tplshape@example.invalid", "name": "模板形状",
		"tags": "vip", "status": string(mailmodel.ContactStatusPending), "consent_source": "",
	})
	if code != http.StatusOK {
		t.Fatalf("模板形状的新建应 200（id 为数字 0），实际 %d（%#v）", code, body)
	}
	row, err := m.GetContactByEmail(ctx, "qa.tplshape@example.invalid")
	if err != nil {
		t.Fatalf("新建后查不到：%v", err)
	}

	// ② 编辑抽屉：id>0（数字）+ 同样的具名字段
	code, body = mailCrudWrite(t, router, "/api/mail/contact/save", map[string]interface{}{
		"id": row.ID, "email": "qa.tplshape@example.invalid", "name": "模板形状改名",
		"tags": "vip, 华南", "status": "", "consent_source": "",
	})
	if code != http.StatusOK {
		t.Fatalf("模板形状的编辑应 200（id 为数字），实际 %d（%#v）", code, body)
	}
	updated, err := m.GetContactByEmail(ctx, "qa.tplshape@example.invalid")
	if err != nil {
		t.Fatalf("编辑后查不到：%v", err)
	}
	if updated.ID != row.ID || updated.Name == nil || *updated.Name != "模板形状改名" {
		t.Fatalf("编辑没落到同一行或姓名未更新：id=%d name=%v", updated.ID, updated.Name)
	}

	// ③ 行内单删（single 档）：身体只有 {id: 数字}
	code, body = mailCrudWrite(t, router, "/api/mail/contact/delete", map[string]interface{}{"id": row.ID})
	if code != http.StatusOK {
		t.Fatalf("模板形状的单删应 200（id 为数字），实际 %d（%#v）", code, body)
	}
	if _, err := m.GetContactByEmail(ctx, "qa.tplshape@example.invalid"); err == nil {
		t.Fatal("单删后联系人应已删除")
	}

	// ④ 反向：修复前那种身体（字符串 id）必须被严格形状拒掉 —— 这是模板声明消失时的报警器。
	if code, body := mailCrudWrite(t, router, "/api/mail/contact/save", map[string]interface{}{
		"id": 0, "email": "qa.tplshape.keep@example.invalid", "name": "对照组",
		"tags": "", "status": string(mailmodel.ContactStatusPending), "consent_source": "",
	}); code != http.StatusOK {
		t.Fatalf("准备对照联系人失败：%d（%#v）", code, body)
	}
	keepRow, err := m.GetContactByEmail(ctx, "qa.tplshape.keep@example.invalid")
	if err != nil {
		t.Fatalf("对照组联系人落库失败：%v", err)
	}
	for _, tc := range []struct {
		name    string
		path    string
		payload map[string]interface{}
	}{
		{"字符串 id 的保存", "/api/mail/contact/save", map[string]interface{}{"id": strconv.FormatUint(keepRow.ID, 10), "email": "qa.tplshape.keep@example.invalid"}},
		{"字符串 id 的单删", "/api/mail/contact/delete", map[string]interface{}{"id": strconv.FormatUint(keepRow.ID, 10)}},
	} {
		code, body := mailCrudWrite(t, router, tc.path, tc.payload)
		if code != http.StatusBadRequest {
			t.Fatalf("%s 应 400（严格形状拒字符串数字），实际 %d（%#v）", tc.name, code, body)
		}
		mailCrudFacingContains(t, mailCrudMsg(t, body), tc.name, "invalidParam", "参数")
	}
	// 被拒的两次都没动数据：对照联系人还在。
	if _, err := m.GetContactByEmail(ctx, "qa.tplshape.keep@example.invalid"); err != nil {
		t.Fatalf("被拒的请求不该动数据，但对照联系人没了：%v", err)
	}
}

// TestMailContactCrudBulkDelete 批量删除：不存在的 id / 非法 id 计入跳过，其余照删。
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

	// 「不存在的 id」必须构造出来而不是猜：本包其它用例（导入批量）会往同一张表里塞上千行，
	// keep.ID+1000 这种偏移量很可能真的存在（实测 1003 就存在）—— 那样断言会变成假绿/假红。
	var maxID uint64
	if err := f.db.Model(&mailmodel.MailContactEntity{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; err != nil {
		t.Fatal(err)
	}
	missing := strconv.FormatUint(maxID+999999, 10) // 已不存在的 id：应计入跳过
	code, body := mailCrudWrite(t, router, "/api/mail/contact/delete", map[string]interface{}{
		"ids": []string{
			strconv.FormatUint(drop1.ID, 10),
			strconv.FormatUint(drop2.ID, 10),
			missing,
			"qa-not-a-number", // 非法 id：也应计入跳过（形状先行，不需要库）
		},
	})
	if code != http.StatusOK {
		t.Fatalf("批量删除应 200，实际 %d（%#v）", code, body)
	}
	data := mailCrudData(t, body)
	if deleted, _ := data["deleted"].(float64); deleted != 2 {
		t.Fatalf("应删掉 2 条，实际 %#v", data)
	}
	if skipped, _ := data["skipped"].(float64); skipped != 2 {
		t.Fatalf("不存在的 id 与非法 id 各计一次跳过，实际 %#v", data)
	}
	items, _ := data["items"].([]interface{})
	if len(items) != 4 {
		t.Fatalf("逐条结果应对应去重后的 4 个 id，实际 %#v", data)
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

	code, body := mailCrudWrite(t, router, "/api/mail/contact/tag", map[string]interface{}{
		"ids":    []string{strconv.FormatUint(row1.ID, 10), strconv.FormatUint(row2.ID, 10)},
		"add":    "VIP2, 华南",
		"remove": "vip",
	})
	if code != http.StatusOK {
		t.Fatalf("批量打标签应 200，实际 %d（%#v）", code, body)
	}
	data := mailCrudData(t, body)
	if updated, _ := data["updated"].(float64); updated != 2 {
		t.Fatalf("非破坏动作的成功计数键是 updated（不是 deleted），实际 %#v", data)
	}
	if _, exists := data["deleted"]; exists {
		t.Fatalf("打标签的摘要不得用 deleted 口径：%#v", data)
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

	code, body := mailCrudWrite(t, router, "/api/mail/contact/tag", map[string]interface{}{
		"ids": []string{strconv.FormatUint(row.ID, 10)}, "add": "", "remove": "",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("空差量应 400，实际 %d（%#v）", code, body)
	}
	mailCrudFacingContains(t, mailCrudMsg(t, body), "空标签批量打标签", "contactTagEmpty", "标签")

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
//
// task-18 起页面的写入口换成了 JSON 声明（data-write-api 落到**按钮**上，见 mail_contacts.html
// 的批量条注释），所以这里断言的是：三个动作各自的端点声明都在、且旧页面 POST 通道真的消失。
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
		`data-drawer-open="#tpl-contact-save"`,      // 页头「新建联系人」
		`id="tpl-contact-save"`,                     // 新建抽屉本体
		`id="tpl-contact-save-`,                     // 行内编辑抽屉
		`data-write-api="/api/mail/contact/save"`,   // 新建 / 编辑抽屉：真端点
		`data-write-api="/api/mail/contact/delete"`, // 行内删除 + 批量删除
		`data-write-mode="single"`,                  // 行内删除的形状
		`data-write-api="/api/mail/contact/tag"`,    // 批量打标签
		`data-write-outcome="update"`,               // 打标签不是破坏动作：不删行
		`data-write-extra="add,remove"`,             // 标签参数按名带上
		`data-write-status`,                         // 就地结果区
		`/admin/mail/contacts/bulk-status`,          // 原有批量改状态仍在（它没有 JSON 端点）
		`id="mk-tag-options"`,                       // 标签候选 datalist
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；正文长度 %d，前 600 字：\n%s", want, len(body), firstN(body, 600))
		}
	}
	for _, gone := range []string{
		"action=\"/admin/mail/contact/save\"",             // 页面 POST 通道：已下线
		"action=\"/admin/mail/contact/delete\"",           // 同上
		"formaction=\"/admin/mail/contacts/bulk-tag\"",    // 批量按钮不再走页面 POST
		"formaction=\"/admin/mail/contacts/bulk-delete\"", // 同上
		"hx-post", // 也不许换一条 HTMX 通道回来
	} {
		if strings.Contains(body, gone) {
			t.Fatalf("页面仍有旧写通道 %q（本批的交付物就是把它删掉）", gone)
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
	if !strings.Contains(body, `id="tpl-contact-save"`) {
		t.Fatal("空态下新建抽屉本体也应渲染（否则入口点了没反应）")
	}
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
