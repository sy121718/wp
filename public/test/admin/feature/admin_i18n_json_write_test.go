package feature

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	adminenums "go_wp/internal/module/admin/enums"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	captcharouter "go_wp/internal/module/common/captcha/router"
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/view"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type i18nWriteFixture struct {
	engine *gin.Engine
	db     *gorm.DB
	sess   *support.AdminSession
	userID string
}

const i18nWriteUser = "i18n_form_write_user"

func newI18nWriteFixture(t *testing.T) *i18nWriteFixture {
	t.Helper()
	fix := &i18nWriteFixture{}

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath: support.NewComponentTestConfig(t), GinMode: gin.TestMode, InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
			e.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			db, err := database.GetDB()
			if err != nil {
				t.Fatalf("取测试库失败：%v", err)
			}
			fix.db = db
			api := e.Group("/api")
			captcharouter.SetupCaptchaRoutes(api)

			adminhttp.SetupAdminRoutes(permission.NewRouteGroup(api), db)
		},
	})
	if err != nil {
		t.Fatalf("装配测试引擎失败：%v", err)
	}
	t.Cleanup(func() {
		if cerr := cleanup(); cerr != nil {
			t.Errorf("清理测试资源失败：%v", cerr)
		}
	})
	fix.engine = engine

	entity := seedAdmin(t, adminmodel.NewAdminModel(fix.db), i18nWriteUser, "i18n-write-pass")
	fix.userID = strconv.FormatUint(entity.ID, 10)

	sess, err := support.LoginAdminSession(t, fix.engine, i18nWriteUser, "i18n-write-pass")
	if err != nil {
		t.Skipf("跳过：真实登录链路不可用（Redis/验证码/会话）：%v", err)
	}
	fix.sess = sess
	return fix
}

func (f *i18nWriteFixture) call(t *testing.T, body string, withSession, withCSRF bool) (int, string) {
	t.Helper()
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}
	if withSession {
		headers["Cookie"] = f.sess.Cookie
	}
	if withCSRF {
		headers["X-CSRF-Token"] = f.sess.CSRFToken
	}
	recorder, err := support.SendRequest(f.engine, support.RequestOptions{
		Method: http.MethodPost, Path: "/api/i18n/save", RawBody: []byte(body), Headers: headers,
	})
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	return recorder.Code, assertI18nHTMLFragment(t, recorder)
}

func (f *i18nWriteFixture) allow(t *testing.T) {
	t.Helper()
	if _, err := pkgcasbin.GetEnforcer().AddPolicy(f.userID, "/api/i18n/save", http.MethodPost, string(permission.I18nManage)); err != nil {
		t.Fatalf("挂写权限策略失败：%v", err)
	}
}

func (f *i18nWriteFixture) deny(t *testing.T) {
	t.Helper()
	if _, err := pkgcasbin.GetEnforcer().RemovePolicy(f.userID, "/api/i18n/save", http.MethodPost, string(permission.I18nManage)); err != nil {
		t.Fatalf("摘写权限策略失败：%v", err)
	}
}

func (f *i18nWriteFixture) entry(t *testing.T, key, lang string) (string, bool) {
	t.Helper()
	got, err := i18n.GetEntry(context.Background(), key, lang)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "record not found") {
			return "", false
		}

		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return "", false
		}
		t.Fatalf("读词条(%s,%s)失败：%v", key, lang, err)
	}
	return got.Value, true
}

func TestAdminI18nFormWriteChain(t *testing.T) {
	fix := newI18nWriteFixture(t)
	const key = "json.write.chain.test"

	if code, _ := fix.call(t, url.Values{"action": {"create"}, "key": {key}, "lang": {"en-US"}, "value": {"v1"}}.Encode(), false, true); code < 400 {
		t.Fatalf("未登录不该被放行，实际 %d", code)
	}

	fix.allow(t)
	if code, _ := fix.call(t, url.Values{"action": {"create"}, "key": {key}, "lang": {"en-US"}, "value": {"v1"}}.Encode(), true, false); code != http.StatusForbidden {
		t.Fatalf("缺 X-CSRF-Token 应被 CSRF 中间件拒（403），实际 %d", code)
	}
	if _, ok := fix.entry(t, key, "en-US"); ok {
		t.Fatal("被 CSRF 拦下的请求不该落库")
	}

	fix.deny(t)
	deniedCode, deniedResp := fix.call(t, url.Values{"action": {"create"}, "key": {key}, "lang": {"en-US"}, "value": {"v1"}}.Encode(), true, true)
	if deniedCode != http.StatusForbidden {
		t.Fatalf("没有 i18n:manage 策略应 403，实际 %d（message=%q）", deniedCode, deniedResp)
	}
	if _, ok := fix.entry(t, key, "en-US"); ok {
		t.Fatal("判权失败不该落库")
	}

	fix.allow(t)
	createdCode, createdResp := fix.call(t, url.Values{"action": {"create"}, "key": {key}, "lang": {"en-US"}, "value": {"v1"}, "category": {"chain"}, "remark": {"r1"}}.Encode(), true, true)
	if createdCode != http.StatusOK {
		t.Fatalf("有策略应放行，实际 %d（message=%q）", createdCode, createdResp)
	}
	if value, ok := fix.entry(t, key, "en-US"); !ok || value != "v1" {
		t.Fatalf("create 应写入词条，实际 value=%q ok=%v", value, ok)
	}

	if code, _ := fix.call(t, url.Values{"action": {"update"}, "key": {key}, "lang": {"en-US"}, "value": {"v2"}}.Encode(), true, true); code != http.StatusOK {
		t.Fatalf("update 已存在的词条应 200，实际 %d", code)
	}
	if value, _ := fix.entry(t, key, "en-US"); value != "v2" {
		t.Fatalf("update 后内容应为 v2，实际 %q", value)
	}
	if code, _ := fix.call(t, url.Values{"action": {"update"}, "key": {key}, "lang": {"fr-FR"}, "value": {"bonjour"}}.Encode(), true, true); code != http.StatusNotFound {
		t.Fatalf("update 缺席的 key×lang 应 404（不静默新建），实际 %d", code)
	}
	if _, ok := fix.entry(t, key, "fr-FR"); ok {
		t.Fatal("update 缺席时不该新建词条")
	}

	if code, _ := fix.call(t, url.Values{"action": {"create"}, "key": {key}, "lang": {"zh-CN"}, "value": {"中文"}}.Encode(), true, true); code != http.StatusOK {
		t.Fatalf("准备批量用例的 zh-CN 词条失败，实际 %d", code)
	}
	// 批量删除：带着坏 ID 就必须整批拒绝，已存在的两条也不能被顺手删掉。
	// 这是 AGENTS.md「批量要么全成要么全败：不推算 skipped、不返回逐条回执」。
	if _, ok := fix.entry(t, key, "zh-CN"); !ok {
		t.Fatal("准备批量用例失败：zh-CN 词条不存在")
	}
	if _, ok := fix.entry(t, key, "en-US"); !ok {
		t.Fatal("准备批量用例失败：en-US 词条不存在")
	}

	badBody := url.Values{"action": {"bulkDelete"}, "ids": {key + "|zh-CN", "no-separator", key + "|en-US"}}.Encode()
	badCode, badResp := fix.call(t, badBody, true, true)
	if badCode != http.StatusBadRequest {
		t.Fatalf("含坏 ID 的批量删除应整批 400，实际 %d（message=%q）", badCode, badResp)
	}
	if strings.Contains(badResp, key) || strings.Contains(badResp, "no-separator") {
		t.Fatalf("拒绝文案不得回显请求里的标识：%q", badResp)
	}
	if _, ok := fix.entry(t, key, "zh-CN"); !ok {
		t.Fatal("整批被拒时 zh-CN 那条必须还在")
	}
	if _, ok := fix.entry(t, key, "en-US"); !ok {
		t.Fatal("整批被拒时 en-US 那条必须还在")
	}

	// 全部合法：两条一起删掉，回执只给条数（含重复项时按去重后计）。
	okBody := url.Values{"action": {"bulkDelete"}, "ids": {key + "|zh-CN", key + "|en-US", key + "|zh-CN"}}.Encode()
	okCode, okResp := fix.call(t, okBody, true, true)
	if okCode != http.StatusOK {
		t.Fatalf("全部合法的批量删除应 200，实际 %d（message=%q）", okCode, okResp)
	}
	if got := i18nHTMLMessage(t, okResp); !strings.HasSuffix(got, "：2") {
		t.Fatalf("批量成功应显示去重后的删除条数 2：%s", okResp)
	}
	if _, ok := fix.entry(t, key, "zh-CN"); ok {
		t.Fatal("批量删除应把 zh-CN 那条删掉")
	}
	if _, ok := fix.entry(t, key, "en-US"); ok {
		t.Fatal("批量删除应把 en-US 那条删掉")
	}

	if code, _ := fix.call(t, url.Values{"action": {"delete"}, "key": {key}, "lang": {"en-US"}}.Encode(), true, true); code != http.StatusOK {
		t.Fatalf("delete 已删除的词条应 200（幂等），实际 %d", code)
	}
	if code, _ := fix.call(t, url.Values{"action": {"delete"}, "key": {key}, "lang": {"en-US"}}.Encode(), true, true); code != http.StatusOK {
		t.Fatalf("重复 delete 应保持 200，实际 %d", code)
	}

	// 查询参数不得补齐写入 body，也不能修改已有译文。
	const guarded = "form.body.only.guard"
	if code, _ := fix.call(t, url.Values{"action": {"create"}, "key": {guarded}, "lang": {"zh-CN"}, "value": {"保留正文"}}.Encode(), true, true); code != http.StatusOK {
		t.Fatal(code)
	}
	for _, tc := range []struct{ contentType, body, query string }{
		{"application/json", `{"action":"update","key":"form.body.only.guard","lang":"zh-CN","value":"错误覆盖"}`, ""},
		{"application/x-www-form-urlencoded", "action=update&key=form.body.only.guard&lang=zh-CN", "?value=错误覆盖"},
	} {
		rec, err := support.SendRequest(fix.engine, support.RequestOptions{Method: http.MethodPost, Path: "/api/i18n/save" + tc.query, RawBody: []byte(tc.body), Headers: map[string]string{"Content-Type": tc.contentType, "Cookie": fix.sess.Cookie, "X-CSRF-Token": fix.sess.CSRFToken}})
		if err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("非表单或 query 补字段应拒绝：%d %s", rec.Code, rec.Body.String())
		}
		assertI18nHTMLFragment(t, rec)
		if value, ok := fix.entry(t, guarded, "zh-CN"); !ok || value != "保留正文" {
			t.Fatalf("拒绝不得改库：%q %v", value, ok)
		}
	}

}

// TestAdminI18nBulkDeleteMissingRollsBackWholeBatch 覆盖「有的一条在、有的不存在」。
//
// 这条是批量语义的核心反例：只要有一条不存在，整批一条都不删 —— 存在的那条也必须
// 还在，并且 HTML 明细要能定位到缺的是哪条（key|lang），不能只说「失败了」。
func TestAdminI18nBulkDeleteMissingRollsBackWholeBatch(t *testing.T) {
	fix := newI18nWriteFixture(t)
	fix.allow(t)

	key := "bulk_missing_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if code, resp := fix.call(t, url.Values{"action": {"create"}, "key": {key}, "lang": {"zh-CN"}, "value": {"保留"}}.Encode(), true, true); code != http.StatusOK {
		t.Fatalf("准备词条失败，实际 %d（message=%q）", code, resp)
	}

	absent := key + "|ja-JP"
	code, resp := fix.call(t, url.Values{"action": {"bulkDelete"}, "ids": {key + "|zh-CN", absent}}.Encode(), true, true)
	if code != http.StatusNotFound {
		t.Fatalf("混入不存在的词条应整批 404，实际 %d（message=%q）", code, resp)
	}
	if _, ok := fix.entry(t, key, "zh-CN"); !ok {
		t.Fatal("整批失败时存在的 zh-CN 那条必须还在（不能部分删除）")
	}

	items := i18nHTMLText(t, resp, "li")
	if len(items) != 1 || !strings.Contains(items[0], "("+absent+")") || strings.Contains(items[0], adminenums.ErrI18nEntryMissing) {
		t.Fatalf("失败明细应只定位缺失语言，并显示已取词原因：%s", resp)
	}
	for _, m := range pageLeakMarkers {
		if strings.Contains(resp, m) {
			t.Fatalf("失败文案不得泄漏内部片段 %q：%q", m, resp)
		}
	}
}

// TestAdminI18nBulkDeleteOnlyRemovesSelectedLanguages 是「按 key 删」这个缺陷的回归。
//
// 同一个 key 在三种语言下是三行；用户只勾了其中两条，第三条必须还在。
// 如果实现退回成 `WHERE item_key IN (...)`，这条会立刻变红。
func TestAdminI18nBulkDeleteOnlyRemovesSelectedLanguages(t *testing.T) {
	fix := newI18nWriteFixture(t)
	fix.allow(t)

	key := "bulk_lang_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	langs := []string{"zh-CN", "en-US", "ja-JP"}
	for _, lang := range langs {
		if code, resp := fix.call(t, url.Values{"action": {"create"}, "key": {key}, "lang": {lang}, "value": {"v-" + lang}}.Encode(), true, true); code != http.StatusOK {
			t.Fatalf("准备词条(%s)失败，实际 %d（message=%q）", lang, code, resp)
		}
	}

	code, resp := fix.call(t, url.Values{"action": {"bulkDelete"}, "ids": {key + "|zh-CN", key + "|en-US"}}.Encode(), true, true)
	if code != http.StatusOK {
		t.Fatalf("只删两门语言应 200，实际 %d（message=%q）", code, resp)
	}
	if got := i18nHTMLMessage(t, resp); !strings.HasSuffix(got, "：2") {
		t.Fatalf("应删除两条：%s", resp)
	}

	if _, ok := fix.entry(t, key, "zh-CN"); ok {
		t.Fatal("选中的 zh-CN 应被删掉")
	}
	if _, ok := fix.entry(t, key, "en-US"); ok {
		t.Fatal("选中的 en-US 应被删掉")
	}
	// 这条是关键：没选中的语言不能被「按 key 删」连带删掉。
	if value, ok := fix.entry(t, key, "ja-JP"); !ok || value != "v-ja-JP" {
		t.Fatalf("未选中的 ja-JP 必须原样保留，实际 value=%q ok=%v", value, ok)
	}
}

// TestAdminI18nBulkDeleteRollsBackOnDatabaseFailure 用事务回调注入故障，
// 证明「锁住了、删到一半失败」也不会留下半截状态。
//
// 注入点是 sys_i18n 的 DELETE 回调：锁与校验都过了、删除语句即将执行时失败，
// 事务回滚后两条词条都必须还在。
func TestAdminI18nBulkDeleteRollsBackOnDatabaseFailure(t *testing.T) {
	fix := newI18nWriteFixture(t)
	fix.allow(t)

	key := "bulk_rollback_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	for _, lang := range []string{"zh-CN", "en-US"} {
		if code, resp := fix.call(t, url.Values{"action": {"create"}, "key": {key}, "lang": {lang}, "value": {"v"}}.Encode(), true, true); code != http.StatusOK {
			t.Fatalf("准备词条(%s)失败，实际 %d（message=%q）", lang, code, resp)
		}
	}

	var failed bool
	cb := fix.db.Callback().Delete()
	const name = "test:inject_i18n_delete_failure"
	if err := cb.Before("gorm:delete").Register(name, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "sys_i18n" {
			failed = true
			_ = tx.AddError(errors.New("注入的删除故障"))
		}
	}); err != nil {
		t.Fatalf("注册删除故障回调失败：%v", err)
	}
	t.Cleanup(func() { _ = cb.Remove(name) })

	code, resp := fix.call(t, url.Values{"action": {"bulkDelete"}, "ids": {key + "|zh-CN", key + "|en-US"}}.Encode(), true, true)
	if !failed {
		t.Fatal("故障注入未生效：sys_i18n 的 DELETE 回调没被触发")
	}
	if code < http.StatusInternalServerError {
		t.Fatalf("数据库故障应至少回 500，实际 %d（message=%q）", code, resp)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		if _, ok := fix.entry(t, key, lang); !ok {
			t.Fatalf("事务回滚后 %s 那条必须还在", lang)
		}
	}
}

func TestAdminI18nFormWriteErrorTextFollowsRequestLanguage(t *testing.T) {
	fix := newI18nWriteFixture(t)
	fix.allow(t)

	const enText, zhText = "I18N-FORM-WRITE-VALUE-REQUIRED-EN", "I18N-FORM-WRITE-VALUE-REQUIRED-ZH"

	type original struct {
		lang  string
		value string
		found bool
	}
	var restore []original
	for _, lang := range []string{"en-US", "zh-CN"} {
		var row struct{ Value string }
		err := fix.db.Raw(`SELECT item_value AS value FROM sys_i18n WHERE item_key = ? AND lang = ?`, adminenums.ErrI18nValueEmpty, lang).Scan(&row).Error
		if err != nil {
			t.Fatalf("读原词条(%s)失败：%v", lang, err)
		}
		restore = append(restore, original{lang: lang, value: row.Value, found: row.Value != ""})
	}
	t.Cleanup(func() {
		for _, row := range restore {
			var err error
			if row.found {
				err = fix.db.Exec(`UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?`,
					row.value, adminenums.ErrI18nValueEmpty, row.lang).Error
			} else {
				err = fix.db.Exec(`DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?`,
					adminenums.ErrI18nValueEmpty, row.lang).Error
			}
			if err != nil {
				t.Errorf("还原词条(%s)失败：%v", row.lang, err)
			}
		}
		if err := i18n.LoadCache(); err != nil {
			t.Errorf("还原后重载词条缓存失败：%v", err)
		}
	})

	for _, row := range []struct{ lang, value string }{{"en-US", enText}, {"zh-CN", zhText}} {
		if err := fix.db.Exec(`INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, update_time)
			VALUES (?, ?, ?, 'test', 'form write chain', 1, 200, NOW())
			ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value`,
			adminenums.ErrI18nValueEmpty, row.lang, row.value).Error; err != nil {
			t.Fatalf("写入合成词条失败：%v", err)
		}
	}
	if err := i18n.LoadCache(); err != nil {
		t.Fatalf("重载词条缓存失败：%v", err)
	}

	body := url.Values{"action": {"create"}, "key": {"json.write.lang.test"}, "lang": {"en-US"}, "value": {"   "}}.Encode()
	phrase := func(accept string) string {
		recorder, err := support.SendRequest(fix.engine, support.RequestOptions{
			Method: http.MethodPost, Path: "/api/i18n/save", RawBody: []byte(body),
			Headers: map[string]string{
				"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json", "Cookie": fix.sess.Cookie,
				"X-CSRF-Token": fix.sess.CSRFToken, "Accept-Language": accept,
			},
		})
		if err != nil {
			t.Fatalf("请求失败：%v", err)
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("内容为空应回 400，实际 %d（%s）", recorder.Code, recorder.Body.String())
		}
		return i18nHTMLMessage(t, assertI18nHTMLFragment(t, recorder))
	}

	if got := phrase("en-US"); got != enText {
		t.Fatalf("en-US 请求应取 en-US 词条（%s），实际 %q", enText, got)
	}
	if got := phrase("zh-CN"); got != zhText {
		t.Fatalf("zh-CN 请求应取 zh-CN 词条（%s），实际 %q", zhText, got)
	}

	if strings.Contains(phrase("en-US"), "sys_i18n") {
		t.Fatal("响应文案里出现了表名 —— 内部细节不应外泄")
	}
}
