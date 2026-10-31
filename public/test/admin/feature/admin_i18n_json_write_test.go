package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strconv"
	"strings"
	"testing"

	adminenums "go_wp/internal/module/admin/enums"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	captcharouter "go_wp/internal/module/common/captcha/router"
	"go_wp/internal/permission"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type i18nWriteResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type i18nWriteBulkData struct {
	Deleted int `json:"deleted"`
	Skipped int `json:"skipped"`
	Items   []struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
		Message string `json:"message"`
	} `json:"items"`
}

type i18nWriteFixture struct {
	engine *gin.Engine
	db     *gorm.DB
	sess   *support.AdminSession
	userID string
}

const i18nWriteUser = "i18n_json_write_user"

func newI18nWriteFixture(t *testing.T) *i18nWriteFixture {
	t.Helper()
	fix := &i18nWriteFixture{}

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath: support.NewComponentTestConfig(t), GinMode: gin.TestMode, InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
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

func (f *i18nWriteFixture) call(t *testing.T, body string, withSession, withCSRF bool) (int, i18nWriteResp) {
	t.Helper()
	headers := map[string]string{"Content-Type": "application/json"}
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
	var resp i18nWriteResp
	if recorder.Body.Len() > 0 {
		if jerr := json.Unmarshal(recorder.Body.Bytes(), &resp); jerr != nil {
			t.Fatalf("响应不是 JSON：%v（%s）", jerr, recorder.Body.String())
		}
	}
	return recorder.Code, resp
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

func TestAdminI18nJSONWriteChain(t *testing.T) {
	fix := newI18nWriteFixture(t)
	const key = "json.write.chain.test"

	if code, _ := fix.call(t, `{"action":"create","key":"`+key+`","lang":"en-US","value":"v1"}`, false, true); code < 400 {
		t.Fatalf("未登录不该被放行，实际 %d", code)
	}

	fix.allow(t)
	if code, _ := fix.call(t, `{"action":"create","key":"`+key+`","lang":"en-US","value":"v1"}`, true, false); code != http.StatusForbidden {
		t.Fatalf("缺 X-CSRF-Token 应被 CSRF 中间件拒（403），实际 %d", code)
	}
	if _, ok := fix.entry(t, key, "en-US"); ok {
		t.Fatal("被 CSRF 拦下的请求不该落库")
	}

	fix.deny(t)
	deniedCode, deniedResp := fix.call(t, `{"action":"create","key":"`+key+`","lang":"en-US","value":"v1"}`, true, true)
	if deniedCode != http.StatusForbidden {
		t.Fatalf("没有 i18n:manage 策略应 403，实际 %d（message=%q）", deniedCode, deniedResp.Message)
	}
	if _, ok := fix.entry(t, key, "en-US"); ok {
		t.Fatal("判权失败不该落库")
	}

	fix.allow(t)
	createdCode, createdResp := fix.call(t, `{"action":"create","key":"`+key+`","lang":"en-US","value":"v1","category":"chain","remark":"r1"}`, true, true)
	if createdCode != http.StatusOK {
		t.Fatalf("有策略应放行，实际 %d（message=%q）", createdCode, createdResp.Message)
	}
	if value, ok := fix.entry(t, key, "en-US"); !ok || value != "v1" {
		t.Fatalf("create 应写入词条，实际 value=%q ok=%v", value, ok)
	}

	if code, _ := fix.call(t, `{"action":"update","key":"`+key+`","lang":"en-US","value":"v2"}`, true, true); code != http.StatusOK {
		t.Fatalf("update 已存在的词条应 200，实际 %d", code)
	}
	if value, _ := fix.entry(t, key, "en-US"); value != "v2" {
		t.Fatalf("update 后内容应为 v2，实际 %q", value)
	}
	if code, _ := fix.call(t, `{"action":"update","key":"`+key+`","lang":"fr-FR","value":"bonjour"}`, true, true); code != http.StatusNotFound {
		t.Fatalf("update 缺席的 key×lang 应 404（不静默新建），实际 %d", code)
	}
	if _, ok := fix.entry(t, key, "fr-FR"); ok {
		t.Fatal("update 缺席时不该新建词条")
	}

	if code, _ := fix.call(t, `{"action":"create","key":"`+key+`","lang":"zh-CN","value":"中文"}`, true, true); code != http.StatusOK {
		t.Fatalf("准备批量用例的 zh-CN 词条失败，实际 %d", code)
	}
	bulkBody := `{"ids":["` + key + `|zh-CN","` + key + `|en-US","no-separator","` + key + `|zh-CN"]}`
	bulkCode, bulkResp := fix.call(t, bulkBody, true, true)
	if bulkCode != http.StatusOK {
		t.Fatalf("批量删除应回 200（部分成功也是 200），实际 %d（message=%q）", bulkCode, bulkResp.Message)
	}
	var bulk i18nWriteBulkData
	if err := json.Unmarshal(bulkResp.Data, &bulk); err != nil {
		t.Fatalf("批量结果 data 解析失败：%v（%s）", err, string(bulkResp.Data))
	}
	if bulk.Deleted != 2 || bulk.Skipped != 1 {
		t.Fatalf("去重后应成功 2 条、跳过 1 条，实际 deleted=%d skipped=%d", bulk.Deleted, bulk.Skipped)
	}
	if len(bulk.Items) != 3 {
		t.Fatalf("逐条结果应覆盖去重后的 3 条，实际 %d 条", len(bulk.Items))
	}
	if _, ok := fix.entry(t, key, "zh-CN"); ok {
		t.Fatal("批量删除应把 zh-CN 那条删掉")
	}

	if code, _ := fix.call(t, `{"action":"delete","key":"`+key+`","lang":"en-US"}`, true, true); code != http.StatusOK {
		t.Fatalf("delete 已删除的词条应 200（幂等），实际 %d", code)
	}
	if code, _ := fix.call(t, `{"action":"delete","key":"`+key+`","lang":"en-US"}`, true, true); code != http.StatusOK {
		t.Fatalf("重复 delete 应保持 200，实际 %d", code)
	}

	if code, _ := fix.call(t, `{"action":"create","key":"`+key+`","lang":"zh-CN","value":"null-guard"}`, true, true); code != http.StatusOK {
		t.Fatalf("准备 null 用例的 zh-CN 词条失败，实际 %d", code)
	}
	nullCode, nullResp := fix.call(t, `{"action":"update","key":"`+key+`","lang":"zh-CN","value":"v9","category":null}`, true, true)
	if nullCode != http.StatusBadRequest {
		t.Fatalf("category 显式 null 应回 400（形状错），实际 %d（message=%q）", nullCode, nullResp.Message)
	}
	if value, ok := fix.entry(t, key, "zh-CN"); !ok || value != "null-guard" {
		t.Fatalf("被形状层拒的请求不该落库（null 不能当空串写进去），实际 value=%q ok=%v", value, ok)
	}
	if code, _ := fix.call(t, `{"ids":null}`, true, true); code != http.StatusBadRequest {
		t.Fatalf("{\"ids\":null} 应回 400（不能当成空批量回 200），实际 %d", code)
	}
	if _, ok := fix.entry(t, key, "zh-CN"); !ok {
		t.Fatal("null 批量档被拒后词条仍应存在（拒绝不能带副作用）")
	}
}

func TestAdminI18nJSONWriteErrorTextFollowsRequestLanguage(t *testing.T) {
	fix := newI18nWriteFixture(t)
	fix.allow(t)

	const enText, zhText = "I18N-JSON-WRITE-VALUE-REQUIRED-EN", "I18N-JSON-WRITE-VALUE-REQUIRED-ZH"

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
			VALUES (?, ?, ?, 'test', 'json write chain', 1, 200, NOW())
			ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value`,
			adminenums.ErrI18nValueEmpty, row.lang, row.value).Error; err != nil {
			t.Fatalf("写入合成词条失败：%v", err)
		}
	}
	if err := i18n.LoadCache(); err != nil {
		t.Fatalf("重载词条缓存失败：%v", err)
	}

	const body = `{"action":"create","key":"json.write.lang.test","lang":"en-US","value":"   "}`
	phrase := func(accept string) string {
		recorder, err := support.SendRequest(fix.engine, support.RequestOptions{
			Method: http.MethodPost, Path: "/api/i18n/save", RawBody: []byte(body),
			Headers: map[string]string{
				"Content-Type": "application/json", "Cookie": fix.sess.Cookie,
				"X-CSRF-Token": fix.sess.CSRFToken, "Accept-Language": accept,
			},
		})
		if err != nil {
			t.Fatalf("请求失败：%v", err)
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("内容为空应回 400，实际 %d（%s）", recorder.Code, recorder.Body.String())
		}
		var resp i18nWriteResp
		if jerr := json.Unmarshal(recorder.Body.Bytes(), &resp); jerr != nil {
			t.Fatalf("响应不是 JSON：%v", jerr)
		}
		return resp.Message
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
