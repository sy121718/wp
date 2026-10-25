package feature

// admin_i18n_json_write_test.go — 词条写端点的**真实链路**回归：
// 路由 → SessionAuth → CSRF → Casbin → handler → PG（外加 pkg/response 的语言取词）。
//
// task-17 把词条页的四种写动作（新建 / 编辑 / 删除 / 批量删除）从页面 POST 收敛到
// `POST /api/i18n/save`（JSON，严格 tagged union：action=create/update/delete，批量档键集恰好 {ids}）。
// 形状判据与错误文案归属由更便宜的用例覆盖（internal/module/admin/inbound/http 的
// admin_i18n_write_test.go，以及同目录 admin_page_err_param_test.go）；本文件只补
// **只有真实链路才答得出来**的三件事：
//
//	① 端点挂的确实是三层链：无会话拒（不是裸 handler）、有会话但缺 X-CSRF-Token 被 CSRF 拒。
//	   前端 write-base.js 会带这个头，缺了它写动作一个都成不了 —— 这条如果只靠「直挂 handler」
//	   的用例是永远测不出来的。
//	② 权限点仍是迁移 178 的 `i18n:manage → POST /api/i18n/save`：没策略 403（且**不落库**）、
//	   挂上策略 200。判权只按 (路径, 方法) 匹配，所以这是「一条 code 一条路径」这条约束的活体验证。
//	③ 四种动作落到库上的效果：create upsert / update 只改已存在的 key×lang（缺席 404，不静默新建）/
//	   delete 幂等 / 批量逐条结果（部分成功仍 200，条数按去重后的口径）。
//
// 依赖 PostgreSQL 与 Redis：本地不可用时 Skip（与 public/test/feature 的登录链路用例同口径），
// 不 Fake、不桩化中间件 —— 本文件的全部价值就在「真实」二字上。

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

// i18nWriteResp pkg/response.Response 的最小解码形态。
type i18nWriteResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// i18nWriteBulkData 批量档的 data 体（前端 write-base.js 读的就是这几个字段）。
type i18nWriteBulkData struct {
	Deleted int `json:"deleted"`
	Skipped int `json:"skipped"`
	Items   []struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
		Message string `json:"message"`
	} `json:"items"`
}

// i18nWriteFixture 真实链路装置：引擎、库、已登录会话、以及该会话对应用户的 id。
type i18nWriteFixture struct {
	engine *gin.Engine
	db     *gorm.DB
	sess   *support.AdminSession
	userID string
}

// i18nWriteUser 本文件专用的**非超管**账号：没有任何 seed 策略，判权只能靠用例自己挂，
// 于是「没策略 403 → 挂上 200」这条回归是确定性的（超管会被 seed 策略抢先放行）。
const i18nWriteUser = "i18n_json_write_user"

// newI18nWriteFixture 装配真实链路。
//
// 用 InitComponents 的 bootstrap（与同目录 admin_i18n_edit_drawer_test.go 同款）：pkg/i18n 与
// pkg/database 的全局端口由组件层注入，词条读写才走真实库；Casbin 与 Redis 同样来自组件层。
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
			// 真实注册：登录口 + 业务 API（含 /api/i18n/save，其分组自带 SessionAuth/CSRF/Casbin）。
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

	// 权限回归要用的账号：is_admin=0（避开超管唯一约束，也不吃任何 seed 策略）。
	entity := seedAdmin(t, adminmodel.NewAdminModel(fix.db), i18nWriteUser, "i18n-write-pass")
	fix.userID = strconv.FormatUint(entity.ID, 10)

	sess, err := support.LoginAdminSession(t, fix.engine, i18nWriteUser, "i18n-write-pass")
	if err != nil {
		t.Skipf("跳过：真实登录链路不可用（Redis/验证码/会话）：%v", err)
	}
	fix.sess = sess
	return fix
}

// call 发一次写请求；withSession=false 模拟未登录，withCSRF=false 模拟前端漏带头。
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

// allow 挂上写权限点（真实 Casbin 策略：sub=用户 id, obj=/api/i18n/save, act=POST）。
func (f *i18nWriteFixture) allow(t *testing.T) {
	t.Helper()
	if _, err := pkgcasbin.GetEnforcer().AddPolicy(f.userID, "/api/i18n/save", http.MethodPost, string(permission.I18nManage)); err != nil {
		t.Fatalf("挂写权限策略失败：%v", err)
	}
}

// deny 摘掉写权限策略（用于「没策略必须 403」那一段；判权只按 (路径, 方法) 匹配）。
func (f *i18nWriteFixture) deny(t *testing.T) {
	t.Helper()
	if _, err := pkgcasbin.GetEnforcer().RemovePolicy(f.userID, "/api/i18n/save", http.MethodPost, string(permission.I18nManage)); err != nil {
		t.Fatalf("摘写权限策略失败：%v", err)
	}
}

// entry 从库里读一条词条（key × lang）。
func (f *i18nWriteFixture) entry(t *testing.T, key, lang string) (string, bool) {
	t.Helper()
	got, err := i18n.GetEntry(context.Background(), key, lang)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "record not found") {
			return "", false
		}
		// 缺行由驱动归一化后的错误文本判定；这里只在真的读不到库时报错。
		if strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return "", false
		}
		t.Fatalf("读词条(%s,%s)失败：%v", key, lang, err)
	}
	return got.Value, true
}

// TestAdminI18nJSONWriteChain 真实链路的写动作回归（三层中间件 + 权限点 + 落库效果）。
func TestAdminI18nJSONWriteChain(t *testing.T) {
	fix := newI18nWriteFixture(t)
	const key = "json.write.chain.test"

	// ① 未登录：三层链的第一层必须拦下（不是裸 handler）。JSON 请求期望 4xx，绝不 2xx。
	if code, _ := fix.call(t, `{"action":"create","key":"`+key+`","lang":"en-US","value":"v1"}`, false, true); code < 400 {
		t.Fatalf("未登录不该被放行，实际 %d", code)
	}
	// ② 有会话但缺 CSRF 头：第二层拦下，且不落库。
	fix.allow(t)
	if code, _ := fix.call(t, `{"action":"create","key":"`+key+`","lang":"en-US","value":"v1"}`, true, false); code != http.StatusForbidden {
		t.Fatalf("缺 X-CSRF-Token 应被 CSRF 中间件拒（403），实际 %d", code)
	}
	if _, ok := fix.entry(t, key, "en-US"); ok {
		t.Fatal("被 CSRF 拦下的请求不该落库")
	}
	// ③ 有会话有 CSRF 但**没有写权限策略**：第三层拦下，且不落库。
	fix.deny(t)
	deniedCode, deniedResp := fix.call(t, `{"action":"create","key":"`+key+`","lang":"en-US","value":"v1"}`, true, true)
	if deniedCode != http.StatusForbidden {
		t.Fatalf("没有 i18n:manage 策略应 403，实际 %d（message=%q）", deniedCode, deniedResp.Message)
	}
	if _, ok := fix.entry(t, key, "en-US"); ok {
		t.Fatal("判权失败不该落库")
	}
	// ④ 挂上策略 → 放行，且 create 真的写进了库（upsert 语义）。
	fix.allow(t)
	createdCode, createdResp := fix.call(t, `{"action":"create","key":"`+key+`","lang":"en-US","value":"v1","category":"chain","remark":"r1"}`, true, true)
	if createdCode != http.StatusOK {
		t.Fatalf("有策略应放行，实际 %d（message=%q）", createdCode, createdResp.Message)
	}
	if value, ok := fix.entry(t, key, "en-US"); !ok || value != "v1" {
		t.Fatalf("create 应写入词条，实际 value=%q ok=%v", value, ok)
	}

	// ⑤ update 只改已存在的 key×lang：存在的改掉，缺席的 404 且**不新建**（抽屉编辑不该悄悄建行）。
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

	// ⑥ 批量档：逐条结果 + 去重/不成形状口径；部分成功仍是 200（前端按 items[].deleted 移除行）。
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

	// ⑦ delete 幂等：再删一次仍是 200（「本来就没了」不是失败），-- 前端重试不会看到假报错。
	if code, _ := fix.call(t, `{"action":"delete","key":"`+key+`","lang":"en-US"}`, true, true); code != http.StatusOK {
		t.Fatalf("delete 已删除的词条应 200（幂等），实际 %d", code)
	}
	if code, _ := fix.call(t, `{"action":"delete","key":"`+key+`","lang":"en-US"}`, true, true); code != http.StatusOK {
		t.Fatalf("重复 delete 应保持 200，实际 %d", code)
	}

	// ⑧ 显式 null 是形状错（400），与「省略」不是一回事：encoding/json 把 null 解进
	// string / []string 都不报错，不判原文就会静默变成「一次空批量」或「把已有分类清空」。
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

// TestAdminI18nJSONWriteErrorTextFollowsRequestLanguage 业务响应文案按请求语言取词。
//
// 词条是**临时插进 sys_i18n 的合成值**（与中文毫无相似度）：响应里出现英文哨兵值，等价于
// 「enums 的 key 真的走到了取词层，且按 Accept-Language 选了语言」—— 本模块若在自己那边
// 翻一遍（或写死中文），这一条会立刻红。
func TestAdminI18nJSONWriteErrorTextFollowsRequestLanguage(t *testing.T) {
	fix := newI18nWriteFixture(t)
	fix.allow(t)

	const enText, zhText = "I18N-JSON-WRITE-VALUE-REQUIRED-EN", "I18N-JSON-WRITE-VALUE-REQUIRED-ZH"
	// 合成词条写进的是**全包共享**的库与**进程级** i18n 缓存：不同步还原，同包其它用例
	//（admin_page_err_param_test.go 的词条缺项用例）会取到这里的哨兵值，以「与本次改动无关的
	// 失败」变红。还原口径与 internal/shell、navigation 的同款用例一致：原值写回，
	// 原本没有的行删掉，最后重载一次缓存。
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

	// 内容为空的 create：命中 ErrI18nValueEmpty（业务档，不是通用 400）。
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
	// 反向参照：内部错误文案不许把原文/表名带出去（受控归口）。
	if strings.Contains(phrase("en-US"), "sys_i18n") {
		t.Fatal("响应文案里出现了表名 —— 内部细节不应外泄")
	}
}
