package feature

// navigation_translation_leak_test.go — 导航译文保存的失败出口不得泄漏数据库原文，也不得出现格式化垃圾。
//
// 缺口原状：保存分支曾是 `data.Errors = []string{"保存失败：" + uerr.Error()}`—— 泄漏的是**页面正文**
// 而不是 JSON 响应体，而 scripts/check-no-internal-error-leak.sh 的判据只看
// response.ErrorWithMessage / c.String 的实参，所以这条一直绿着、泄漏一直在。
//
// 保存迁到真实 JSON 写接口（POST /api/navigation/update，action=translations）后，出口变成
// response 的 message：判据不变 —— 任何内部细节（表名 / SQLSTATE / 约束名）都不许出现在
// 客户端可见的文案里，且业务句必须按**请求语言**给成品。
//
// 本用例从真实失败形态出发：把 sys_translation 改名，让 Upsert 撞上 PG 的
// `relation "sys_translation" does not exist (SQLSTATE 42P01)`—— 正是「表名 + SQLSTATE」那一类内部细节。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

// translationLeakTokens 响应体里的内部细节指纹。
//
// 与 navigation_error_leak_test.go 的 JSON 判据分开：那里能拿 "navigations" / "uuid" 当指纹，
// 而这里的响应体本来就带 `navigation.label` 语境名，不能进这份清单。
var translationLeakTokens = []string{
	"SQLSTATE", "42P01", "sys_translation", `relation "`, "uq_", "pg_", "constraint",
}

// translationWriteRequest 造一次译文保存请求（真实 JSON 形状）。
func translationWriteRequest(projectID, lang, sourceText, target string) *http.Request {
	body := fmt.Sprintf(
		`{"action":"translations","project":%q,"lang":%q,"rows":[{"context":%q,"hash":%q,"target":%q}]}`,
		projectID, lang, i18n.ContentContext("navigation", "label"), i18n.ContentHash(sourceText), target)
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestNavigationTranslationSaveHidesInternalError 写入失败：归口文案 + 无数据库原文，且按请求语言给成品。
func TestNavigationTranslationSaveHidesInternalError(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	// 真实失败形态：目标表不存在。PG 的原文里同时带表名与 SQLSTATE 码。
	if err := db.Exec("ALTER TABLE sys_translation RENAME TO sys_translation_leak_probe").Error; err != nil {
		t.Fatalf("制造写入失败场景失败: %v", err)
	}

	// 反证：这条失败路径的**原始错误**确实带表名与 SQLSTATE。
	// 少了这一步，「响应没泄漏」可能只是「错误里本来就没有内部细节」—— 断言等于没断言。
	writer := i18n.NewContentWriter(db)
	_, uerr := writer.Upsert(context.Background(), []i18n.ContentWriteItem{{
		SourceHash: i18n.ContentHash("首页"), Context: i18n.ContentContext("navigation", "label"),
		Lang: "en-US", SourceText: "首页", TargetText: "Home", Engine: i18n.ContentEngineManual,
	}})
	if uerr == nil || !strings.Contains(uerr.Error(), "sys_translation") {
		t.Fatalf("夹具不符：写入失败的原始错误应带表名，实际 %v", uerr)
	}
	t.Logf("原始错误（只应进日志）: %v", uerr)

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := navigationhttp.NewNavigationTranslationHandle(navSvc, projects)
	handle.SetContentWriter(writer)
	navigationhttp.SetNavigationTranslationWriter(handle)
	t.Cleanup(func() { navigationhttp.SetNavigationTranslationWriter(nil) })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/navigation/update", navigationhttp.NewHandle(navSvc).Update)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, translationWriteRequest(projectID, "en-US", "首页", "Home"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("写入失败应 500（归口），实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}
	payload := recorder.Body.String()
	// 先证明真的走到了失败分支：没有这句断言，一条「什么都没渲染」的空响应也能通过下面的检查。
	// 本测试进程里 i18n 缓存是空的（没有从库里加载词条），所以落下来的就是词条的**中文兜底成品句**
	// —— 「按请求语言取词」这件事由 internal/module/navigation/inbound/http 的
	// navigation_translation_write_test.go 用注入词条的用例单独钉住。
	if !strings.Contains(payload, "保存失败") {
		t.Fatalf("失败分支应给出归口句，实际 body=%s", payload)
	}
	for _, tok := range translationLeakTokens {
		if strings.Contains(payload, tok) {
			t.Errorf("响应泄漏内部细节 %q: %s", tok, payload)
		}
	}
	// 词条的中英占位符写法不同（zh 用 %s、en 用 {detail}）：填错就会出现
	// `Save failed: {detail}%!(EXTRA string=…)` —— 那串垃圾本身也是「对外文案不对」。
	if strings.Contains(payload, "%!") || strings.Contains(payload, "{detail}") {
		t.Errorf("归口句出现格式化垃圾（占位符没按语言填）: %s", payload)
	}
}

// TestNavigationTranslationSaveMessageIsLocalizedWithoutFormatGarbage 成功路径：成品句按请求语言给，占位符被填掉。
func TestNavigationTranslationSaveMessageIsLocalizedWithoutFormatGarbage(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := navigationhttp.NewNavigationTranslationHandle(navSvc, projects)
	handle.SetContentWriter(i18n.NewContentWriter(db))
	navigationhttp.SetNavigationTranslationWriter(handle)
	t.Cleanup(func() { navigationhttp.SetNavigationTranslationWriter(nil) })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/navigation/update", navigationhttp.NewHandle(navSvc).Update)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, translationWriteRequest(projectID, "en-US", "首页", "Home"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("保存应 200，实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}
	payload := recorder.Body.String()
	if !strings.Contains(payload, "已保存 1 条译文") || !strings.Contains(payload, `"written":1`) {
		t.Fatalf("成功响应应带写入计数与成品句（本进程无词条缓存 → 中文兜底），实际 %s", payload)
	}
	if strings.Contains(payload, "%!") {
		t.Errorf("成功句出现格式化垃圾（占位符没按语言填）: %s", payload)
	}
	// 真正落的库：译文行存在（否则「written:1」只是一句自述）。
	var target string
	row := db.Raw("SELECT target_text FROM sys_translation WHERE source_hash = ? AND lang = 'en-US'",
		i18n.ContentHash("首页")).Scan(&target)
	if row.Error != nil || target != "Home" {
		t.Fatalf("译文未落库: err=%v target=%q", row.Error, target)
	}
}

// navTranslationSpyMarker / navTranslationSpyMenus 观察「本次保存是否派发了失效」。
//
// 这两个端口的接口类型在模块内未导出，但方法集是公开契约：测试包给出自己的实现即可，
// 装配期注入后就能断言「被拒的请求一行都没往外扩散」。
type navTranslationSpyMarker struct{ calls int }

func (s *navTranslationSpyMarker) MarkStaleForI18n(context.Context) error {
	s.calls++
	return nil
}

type navTranslationSpyMenus struct{ kinds []string }

func (s *navTranslationSpyMenus) InvalidateMenuLabels(_ context.Context, _ string, kinds []string) {
	s.kinds = append(s.kinds, kinds...)
}

// translationWriteRaw 直接发一段 JSON body —— 形状用例要自己造畸形体，不能走构造器。
func translationWriteRaw(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/navigation/update", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

// translationRowCount 某原文哈希在指定语言下的译文行数。
func translationRowCount(t *testing.T, db *gorm.DB, sourceText, lang string) int64 {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT count(*) FROM sys_translation WHERE source_hash = ? AND lang = ?",
		i18n.ContentHash(sourceText), lang).Scan(&count).Error; err != nil {
		t.Fatalf("查询译文行失败: %v", err)
	}
	return count
}

// TestNavigationTranslationRowKeysAreRequired 行缺键必须 400，且**不写库、不派发失效**；
// 显式空 target 才是契约里的「本次不改动这一行」。
//
// 为什么两条要挨着测：判据卡的是「键是否存在」，不是「值是否为空」。只测缺键，把判据写成
// 「target 非空才算合法」也会绿 —— 那会让「清空某条译文」这种合法意图变成 400。
// 只测空 target，缺键被静默吞成 200 + written:0 也不会红（这正是 Sol 查出的 P2）。
func TestNavigationTranslationRowKeysAreRequired(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := navigationhttp.NewNavigationTranslationHandle(navSvc, projects)
	handle.SetContentWriter(i18n.NewContentWriter(db))
	marker := &navTranslationSpyMarker{}
	menus := &navTranslationSpyMenus{}
	handle.SetPageMarker(marker)
	handle.SetMenuInvalidator(menus)
	navigationhttp.SetNavigationTranslationWriter(handle)
	t.Cleanup(func() { navigationhttp.SetNavigationTranslationWriter(nil) })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/navigation/update", navigationhttp.NewHandle(navSvc).Update)

	context := i18n.ContentContext("navigation", "label")
	hash := i18n.ContentHash("首页")
	wrap := func(rows string) string {
		return fmt.Sprintf(`{"action":"translations","project":%q,"lang":"en-US","rows":%s}`, projectID, rows)
	}

	// 1) 缺键：三种都必须是 400，且既不落库也不派发失效。
	for _, tc := range []struct{ name, rows string }{
		{"缺 target", fmt.Sprintf(`[{"context":%q,"hash":%q}]`, context, hash)},
		{"缺 context", fmt.Sprintf(`[{"hash":%q,"target":"Home"}]`, hash)},
		{"缺 hash", fmt.Sprintf(`[{"context":%q,"target":"Home"}]`, context)},
		{"第二行缺 target", fmt.Sprintf(`[{"context":%q,"hash":%q,"target":"Home"},{"context":%q,"hash":"h-2"}]`,
			context, hash, context)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := translationWriteRaw(t, router, wrap(tc.rows))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("缺键的行应 400（否则会被当成空译文静默跳过），实际 %d body=%s",
					recorder.Code, recorder.Body.String())
			}
			if got := translationRowCount(t, db, "首页", "en-US"); got != 0 {
				t.Fatalf("被拒的请求不该写库，但已有 %d 行译文", got)
			}
			if marker.calls != 0 || len(menus.kinds) != 0 {
				t.Fatalf("被拒的请求不该派发失效：pageMarker=%d kinds=%v", marker.calls, menus.kinds)
			}
		})
	}

	// 2) 显式空 target：合法形状（「本次不改动这一行」），200 且零写入、零派发。
	empty := translationWriteRaw(t, router, wrap(fmt.Sprintf(`[{"context":%q,"hash":%q,"target":""}]`, context, hash)))
	if empty.Code != http.StatusOK {
		t.Fatalf("显式空 target 应被接受（本次不改动），实际 %d body=%s", empty.Code, empty.Body.String())
	}
	if !strings.Contains(empty.Body.String(), `"written":0`) {
		t.Fatalf("空译文不该产生写入，实际 %s", empty.Body.String())
	}
	if got := translationRowCount(t, db, "首页", "en-US"); got != 0 {
		t.Fatalf("空译文不该写库，但已有 %d 行", got)
	}
	if marker.calls != 0 || len(menus.kinds) != 0 {
		t.Fatalf("没有真正变更时不该派发失效：pageMarker=%d kinds=%v", marker.calls, menus.kinds)
	}

	// 3) 正对照：同一形状、非空 target → 200、真落库、真派发失效。
	// 没有这一条，上面两条「没写库、没派发」也可能只是整条链路坏了。
	written := translationWriteRaw(t, router, wrap(fmt.Sprintf(`[{"context":%q,"hash":%q,"target":"Home"}]`, context, hash)))
	if written.Code != http.StatusOK {
		t.Fatalf("合法写入应 200，实际 %d body=%s", written.Code, written.Body.String())
	}
	if got := translationRowCount(t, db, "首页", "en-US"); got != 1 {
		t.Fatalf("合法写入应落 1 行译文，实际 %d", got)
	}
	if marker.calls == 0 || len(menus.kinds) == 0 {
		t.Fatalf("真正变更后必须派发失效：pageMarker=%d kinds=%v", marker.calls, menus.kinds)
	}
}
