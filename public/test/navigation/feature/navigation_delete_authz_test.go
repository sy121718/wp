package feature

// navigation_delete_authz_test.go — 导航「新建 / 单条删除 / 批量删除」迁到真实 JSON 端点后的用户契约。
//
// 契约链：Session（会话 Cookie）→ CSRF（X-CSRF-Token）→ Casbin（navigation:create / navigation:delete，
// 迁移 036 落的权限点）→ handler → PG。本文件补的正是链路层：模块内用例**不带中间件**
// （直接 POST 到 gin 路由），能证明形状与取词，却证明不了「无策略会被拦住」「缺令牌发不出去」
// 「旧页面 POST 真的不在了」。装置与 internal/routers/assembly.go 同构地装配 /api 的三层链 ——
// 装置与生产不同构时，断言的是另一个系统。
//
// 复用 navigation_translation_authz_test.go 的装置（同一份 /api 三层链 + 超管/普通管理员两条会话）：
// 两个文件验的是同一个装配形态，各建一套装置只会让「同构」这句话有两个版本。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/i18n"
	"go_wp/public/test/support"
)

// 迁到 JSON 端点的两条写路径（权限点与 sys_permission.api_path 逐字一致，判权只看 (obj, act)）。
const (
	navCreateAPIPath = "/api/navigation/create"
	navDeleteAPIPath = "/api/navigation/delete"

	navCreatePermission = "navigation:create"
	navDeletePermission = "navigation:delete"
)

// navWriteAuthzPost 发 JSON 写请求；withCSRF=false 时**不发** X-CSRF-Token
// （模拟「前端忘了带令牌」：会话有效、令牌缺失）。
func navWriteAuthzPost(t *testing.T, env *navTranslationAuthzEnv, sess *support.AdminSession,
	path string, payload map[string]interface{}, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化 %s 的请求体失败: %v", path, err)
	}
	headers := map[string]string{
		"Cookie":       sess.Cookie,
		"Content-Type": "application/json",
	}
	if withCSRF {
		headers["X-CSRF-Token"] = sess.CSRFToken
	}
	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodPost, Path: path, Headers: headers, RawBody: raw,
	})
	if err != nil {
		t.Fatalf("发送 POST %s 失败: %v", path, err)
	}
	return recorder
}

// navWriteAuthzPostLang 与 navWriteAuthzPost 同形，另带 Accept-Language：
// 逐条原因是**按请求语言**渲染的成品句，不带这一步只能测到默认语言那一支。
func navWriteAuthzPostLang(t *testing.T, env *navTranslationAuthzEnv, sess *support.AdminSession,
	path string, payload map[string]interface{}, lang string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化 %s 的请求体失败: %v", path, err)
	}
	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodPost, Path: path,
		Headers: map[string]string{
			"Cookie":          sess.Cookie,
			"X-CSRF-Token":    sess.CSRFToken,
			"Content-Type":    "application/json",
			"Accept-Language": lang,
		},
		RawBody: raw,
	})
	if err != nil {
		t.Fatalf("发送 POST %s 失败: %v", path, err)
	}
	return recorder
}

// navSeedText 读 sys_i18n 里某条词条在某语言下的原文（seed 真源；期望值不写死在用例里）。
func navSeedText(t *testing.T, env *navTranslationAuthzEnv, key, lang string) string {
	t.Helper()
	var value string
	if err := env.DB.Raw(`SELECT item_value FROM sys_i18n WHERE item_key = ? AND lang = ?`, key, lang).
		Scan(&value).Error; err != nil {
		t.Fatalf("读 sys_i18n 词条失败 key=%s lang=%s: %v", key, lang, err)
	}
	if strings.TrimSpace(value) == "" {
		t.Fatalf("sys_i18n 里没有 key=%s lang=%s 的词条（迁移没跑全？）", key, lang)
	}
	return value
}

// navFindBulkItem 从批档回执的 items 里取出某条 id 的逐条结果。
func navFindBulkItem(t *testing.T, items []interface{}, id string) map[string]interface{} {
	t.Helper()
	for _, raw := range items {
		item, _ := raw.(map[string]interface{})
		if item["id"] == id {
			return item
		}
	}
	t.Fatalf("批档回执里没有 id=%s 的逐条结果: %v", id, items)
	return nil
}

// navAssertNoLeak 逐条原因既不得出现数据库/内部细节，也不得是内部归口 key。
func navAssertNoLeak(t *testing.T, message string) {
	t.Helper()
	for _, tok := range []string{"SQLSTATE", "pg_", "constraint", "record not found", navInternalKey} {
		if strings.Contains(message, tok) {
			t.Errorf("逐条原因泄漏内部细节 %q：%q", tok, message)
		}
	}
}

// navItemExists 用**契约**问一条导航项是否还在（不是直接查表：判据与生产读路径同一份）。
func navItemExists(t *testing.T, env *navTranslationAuthzEnv, id string) bool {
	t.Helper()
	item, err := env.NavSvc.Get(context.Background(), &navigationdto.GetReq{ID: id})
	if err != nil || item == nil {
		return false
	}
	return item.ID == id
}

// navWriteAuthzPolicyCount 普通管理员手上这条 (obj, code) 的策略条数。
func navWriteAuthzPolicyCount(t *testing.T, env *navTranslationAuthzEnv, path, perm string) int64 {
	t.Helper()
	var count int64
	if err := env.DB.Raw(`SELECT count(*) FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`,
		strconv.FormatInt(env.PlainID, 10), path, perm).Scan(&count).Error; err != nil {
		t.Fatalf("查询普通管理员策略失败: %v", err)
	}
	return count
}

// navWriteAuthzSetPolicy 增删后重载策略：Casbin 是内存快照，只改库不会改变判权结果。
func navWriteAuthzSetPolicy(t *testing.T, env *navTranslationAuthzEnv, path, perm string, grant bool) {
	t.Helper()
	var err error
	if grant {
		err = env.DB.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
			VALUES ('p', ?, ?, 'POST', ?)
			ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`,
			strconv.FormatInt(env.PlainID, 10), path, perm).Error
	} else {
		err = env.DB.Exec(`DELETE FROM sys_casbin_rule
			WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`,
			strconv.FormatInt(env.PlainID, 10), path, perm).Error
	}
	if err != nil {
		t.Fatalf("变更普通管理员策略失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载 Casbin 策略失败: %v", err)
	}
}

// TestNavigationDeleteWriteAuthzChain 无策略 403、超管放行、补策略即放行 ——
// 「403 由这条 Casbin 路径决定」用因果反证钉住（单档与批档是同一条路径，故一并验）。
func TestNavigationDeleteWriteAuthzChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	// 前提：普通管理员没有任何 navigation:delete 策略（否则下面的 403 反例不成立）。
	navWriteAuthzSetPolicy(t, env, navDeleteAPIPath, navDeletePermission, false)
	if got := navWriteAuthzPolicyCount(t, env, navDeleteAPIPath, navDeletePermission); got != 0 {
		t.Fatalf("反例前提不成立：普通管理员仍有 %d 条 %s 策略", got, navDeletePermission)
	}

	victim := addNavItem(t, env.NavSvc, env.ProjectID, "待删项", "/victim", nil, "self")

	// 1) 反例：无策略 —— 被 Casbin 拦在 handler 之前（不是形状错、不是 500）。
	denied := navWriteAuthzPost(t, env, env.Plain, navDeleteAPIPath,
		map[string]interface{}{"id": victim}, true)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员应 403，实际 %d: %s", denied.Code, denied.Body.String())
	}
	// 被拦下的请求不能留下任何痕迹：403 只是「没进去」的旁证，行还在才是效果。
	if !navItemExists(t, env, victim) {
		t.Fatalf("403 的请求不该真删掉行（id=%s）", victim)
	}

	// 2) 超管对照：策略由 seed 通道按 is_admin=1 补全 —— 必须 200 且真删。
	asAdmin := navWriteAuthzPost(t, env, env.Super, navDeleteAPIPath,
		map[string]interface{}{"id": victim}, true)
	if asAdmin.Code != http.StatusOK {
		t.Fatalf("超管删除应 200，实际 %d: %s", asAdmin.Code, asAdmin.Body.String())
	}
	if navItemExists(t, env, victim) {
		t.Fatalf("超管删除后行仍存在（id=%s）", victim)
	}

	// 3) 批档同一条路径：无策略同样 403 且两条都不删（批档不是另一条权限锚点）。
	batchA := addNavItem(t, env.NavSvc, env.ProjectID, "批删 A", "/batch-a", nil, "self")
	batchB := addNavItem(t, env.NavSvc, env.ProjectID, "批删 B", "/batch-b", nil, "self")
	deniedBulk := navWriteAuthzPost(t, env, env.Plain, navDeleteAPIPath,
		map[string]interface{}{"ids": []string{batchA, batchB}}, true)
	if deniedBulk.Code != http.StatusForbidden {
		t.Fatalf("无策略的批量删除应 403，实际 %d: %s", deniedBulk.Code, deniedBulk.Body.String())
	}
	if !navItemExists(t, env, batchA) || !navItemExists(t, env, batchB) {
		t.Fatal("403 的批量删除不该删掉任何一行")
	}

	// 4) 因果反证：补上策略后同一请求立刻放行，且逐条结果带计数。
	navWriteAuthzSetPolicy(t, env, navDeleteAPIPath, navDeletePermission, true)
	allowed := navWriteAuthzPost(t, env, env.Plain, navDeleteAPIPath,
		map[string]interface{}{"ids": []string{batchA, batchB}}, true)
	if allowed.Code != http.StatusOK {
		t.Fatalf("补上策略后普通管理员应 200（说明 403 由该条 Casbin 路径决定），实际 %d: %s",
			allowed.Code, allowed.Body.String())
	}
	body := map[string]interface{}{}
	if err := json.Unmarshal(allowed.Body.Bytes(), &body); err != nil {
		t.Fatalf("批量回执不是 JSON: %s", allowed.Body.String())
	}
	data, _ := body["data"].(map[string]interface{})
	if data == nil || data["deleted"] != float64(2) || data["skipped"] != float64(0) {
		t.Fatalf("批量回执应为 deleted=2 skipped=0，实际 %s", allowed.Body.String())
	}
	if navItemExists(t, env, batchA) || navItemExists(t, env, batchB) {
		t.Fatal("补策略后的批量删除应真的删掉两行")
	}
}

// TestNavigationCreateWriteAuthzChain 新建同样落在真实权限点上：无 navigation:create 被拦，
// 超管放行且行真的落库（含抽屉提交的那组字段形态）。
func TestNavigationCreateWriteAuthzChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	navWriteAuthzSetPolicy(t, env, navCreateAPIPath, navCreatePermission, false)

	payload := map[string]interface{}{
		"projectId": env.ProjectID,
		"kind":      "header",
		"title":     "授权链新建项",
		"path":      "/authz-create",
		"target":    "self",
	}

	denied := navWriteAuthzPost(t, env, env.Plain, navCreateAPIPath, payload, true)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员新建应 403，实际 %d: %s", denied.Code, denied.Body.String())
	}
	if items, err := env.NavSvc.List(context.Background(),
		&navigationdto.ListReq{ProjectID: env.ProjectID, Kind: "header"}); err == nil {
		for _, item := range items {
			if item.Title == "授权链新建项" {
				t.Fatalf("403 的新建请求不该落库，但已存在同名项（id=%s）", item.ID)
			}
		}
	}

	navWriteAuthzSetPolicy(t, env, navCreateAPIPath, navCreatePermission, true)
	allowed := navWriteAuthzPost(t, env, env.Plain, navCreateAPIPath, payload, true)
	if allowed.Code != http.StatusOK {
		t.Fatalf("补上策略后新建应 200（说明 403 由该条 Casbin 路径决定），实际 %d: %s",
			allowed.Code, allowed.Body.String())
	}
	created := navItemByTitle(t, env, "授权链新建项")
	if created == "" {
		t.Fatalf("新建成功但行没落库：%s", allowed.Body.String())
	}
	if !navItemExists(t, env, created) {
		t.Fatalf("新建的行读不回来（id=%s）", created)
	}
}

// navItemByTitle 按标题在 header 位置里找一条导航项（找不到返回空串）。
func navItemByTitle(t *testing.T, env *navTranslationAuthzEnv, title string) string {
	t.Helper()
	items, err := env.NavSvc.List(context.Background(),
		&navigationdto.ListReq{ProjectID: env.ProjectID, Kind: "header"})
	if err != nil {
		t.Fatalf("列导航项失败: %v", err)
	}
	for _, item := range items {
		if item.Title == title {
			return item.ID
		}
	}
	return ""
}

// TestNavigationWriteRequiresCSRF 会话有效但没带令牌：CSRF 层拒绝，且不落库。
//
// 这一档在模块内测不出来（那里的路由没有中间件）：CSRF 是**装配层**的中间件，
// 少了它，「模板去掉 csrf 隐藏域」这类改动不会有任何测试变红 —— 而切片 A 恰好删掉了
// 三个隐藏域（页面 POST 退役），必须有人在链路层守住。
func TestNavigationWriteRequiresCSRF(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	victim := addNavItem(t, env.NavSvc, env.ProjectID, "CSRF 待删项", "/csrf-victim", nil, "self")

	withoutToken := navWriteAuthzPost(t, env, env.Super, navDeleteAPIPath,
		map[string]interface{}{"id": victim}, false)
	// 缺令牌的契约状态码就是 403（builtin.CSRFMiddleware：缺失或不一致 → 403）；
	// 只写「不等于 200」会把 400/500 也算通过 —— 那两种恰好是「请求进了 handler」的形态。
	if withoutToken.Code != http.StatusForbidden {
		t.Fatalf("缺少 X-CSRF-Token 的写请求应 403，实际 %d: %s", withoutToken.Code, withoutToken.Body.String())
	}
	if !navItemExists(t, env, victim) {
		t.Fatal("被 CSRF 拦下的请求不该真删掉行")
	}

	withToken := navWriteAuthzPost(t, env, env.Super, navDeleteAPIPath,
		map[string]interface{}{"id": victim}, true)
	if withToken.Code != http.StatusOK {
		t.Fatalf("补上令牌的同一请求应 200，实际 %d: %s", withToken.Code, withToken.Body.String())
	}
	if navItemExists(t, env, victim) {
		t.Fatal("补令牌后的删除应真的删掉行")
	}
}

// TestNavigationDeleteBulkResultFromRealRows 批档的业务结果（真实 PG + 真实链路）：
// 成功的真删、失败的计入 skipped 并带上**业务原因**（不是数据库原文），未涉及的行业原样在。
//
// 模块内的批档用例用桩 service，只能证明「逐条调用 + 结果组装」；这里证明的是
// 「结果与库里的真实变化一致」—— 两者都缺一不可。
func TestNavigationDeleteBulkResultFromRealRows(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	goneA := addNavItem(t, env.NavSvc, env.ProjectID, "批删甲", "/bulk-gone-a", nil, "self")
	goneB := addNavItem(t, env.NavSvc, env.ProjectID, "批删乙", "/bulk-gone-b", nil, "self")
	kept := addNavItem(t, env.NavSvc, env.ProjectID, "不删项", "/bulk-keep", nil, "self")

	// 一个不存在的 id 混在批里：它必须被计入 skipped（逐条失败不中断整批）。
	const missing = "00000000-0000-0000-0000-0000000000ff"
	recorder := navWriteAuthzPost(t, env, env.Super, navDeleteAPIPath,
		map[string]interface{}{"ids": []string{goneA, missing, goneB}}, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("批档应 200（逐条结果在 data 里），实际 %d: %s", recorder.Code, recorder.Body.String())
	}

	body := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("回执不是 JSON: %s", recorder.Body.String())
	}
	data, _ := body["data"].(map[string]interface{})
	if data == nil {
		t.Fatalf("回执缺少 data 段: %s", recorder.Body.String())
	}
	if data["deleted"] != float64(2) || data["skipped"] != float64(1) {
		t.Fatalf("回执应为 deleted=2 skipped=1，实际 %s", recorder.Body.String())
	}
	items, _ := data["items"].([]interface{})
	if len(items) != 3 {
		t.Fatalf("逐条结果应有 3 条（含失败那条），实际 %d 条: %s", len(items), recorder.Body.String())
	}
	// 失败那一条的原因必须是**成品句**（本模块归口的业务文案），既不是数据库原文、也不是裸 key。
	//
	// 本进程没有加载词条缓存（该包所有用例的默认状态：GetText 未命中即原样返回 key），
	// 所以这里走的正是「词条缺席 → 本模块中文兜底」那一支；期望值从 sys_i18n 读 zh-CN 原文，
	// 于是这条断言同时钉住两件事：兜底句与词条**逐字一致**、响应里出现的不是裸 key。
	seedText := navSeedText(t, env, navigationenums.ErrNotFound, "zh-CN")
	failed := navFindBulkItem(t, items, missing)
	if failed["deleted"] != false {
		t.Fatalf("不存在的 id 不该被标记为已删除: %v", failed)
	}
	message, _ := failed["message"].(string)
	if message != seedText {
		t.Fatalf("逐条原因应为成品句 %q（词条缺席时由本模块兜底），实际 %q", seedText, message)
	}
	navAssertNoLeak(t, message)

	// 库里的真实变化：两条真删、第三条原样在。
	if navItemExists(t, env, goneA) || navItemExists(t, env, goneB) {
		t.Fatal("成功那两条应真的从库里消失")
	}
	if !navItemExists(t, env, kept) {
		t.Fatal("没参与本次批量的行不该被删")
	}
}

// TestNavigationDeleteSingleRemovesDescendants 单删回归（真实 PG + 真实链路）：
// 删父项必须把子项一并删掉 —— 导航树是一个聚合，留下孤儿节点会被渲染成顶级项。
//
// 这条语义由 service 决定（`navDescendantIDs`），JSON 迁移**没有**改它；本用例的作用是
// 把「迁移后仍是整棵子树删除」钉住 —— 只断言 200 的话，退化成「只删自己」也不会红。
func TestNavigationDeleteSingleRemovesDescendants(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	parent := addNavItem(t, env.NavSvc, env.ProjectID, "父项", "/tree-parent", nil, "self")
	child := addNavItem(t, env.NavSvc, env.ProjectID, "子项", "/tree-child", &parent, "self")
	grandchild := addNavItem(t, env.NavSvc, env.ProjectID, "孙项", "/tree-grandchild", &child, "self")
	sibling := addNavItem(t, env.NavSvc, env.ProjectID, "旁支", "/tree-sibling", nil, "self")

	recorder := navWriteAuthzPost(t, env, env.Super, navDeleteAPIPath,
		map[string]interface{}{"id": parent}, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("单删应 200，实际 %d: %s", recorder.Code, recorder.Body.String())
	}

	for _, id := range []string{parent, child, grandchild} {
		if navItemExists(t, env, id) {
			t.Fatalf("删父项应连带删掉整棵子树（id=%s 仍在）", id)
		}
	}
	if !navItemExists(t, env, sibling) {
		t.Fatal("旁支不该被这次单删波及")
	}
}

// TestNavigationLegacyPagePostsAreGone 三条旧页面写路由已下线，列表页仍在且写声明指向真实端点。
//
// 两件事必须一起钉：只断言旧路由 404，改坏页面装配（路由整体没挂上）也不会红；
// 只断言页面 200，则旧路由复活（回到两套写入口）也不会红。
func TestNavigationLegacyPagePostsAreGone(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	for _, legacy := range []string{
		"/admin/navigations/create",
		"/admin/navigations/delete",
		"/admin/navigations/bulk-delete",
	} {
		recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
			Method: http.MethodPost,
			Path:   legacy,
			Headers: map[string]string{
				"Cookie":       env.Super.Cookie,
				"X-CSRF-Token": env.Super.CSRFToken,
				"Content-Type": "application/x-www-form-urlencoded",
			},
			RawBody: []byte("projectId=" + env.ProjectID + "&kind=header&title=t&path=/t"),
		})
		if err != nil {
			t.Fatalf("发送旧页面 POST %s 失败: %v", legacy, err)
		}
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("旧页面写路由 %s 应 404，实际 %d: %s", legacy, recorder.Code, recorder.Body.String())
		}
	}

	page, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/admin/navigations?project=" + env.ProjectID + "&kind=header",
		Headers: map[string]string{
			"Cookie": env.Super.Cookie,
			"Accept": "text/html,application/xhtml+xml",
		},
	})
	if err != nil {
		t.Fatalf("发送列表页 GET 失败: %v", err)
	}
	if page.Code != http.StatusOK {
		t.Fatalf("导航列表页应 200，实际 %d: %s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	// 页面上的写契约必须与真实端点一致：点一次新建 / 删除就该打到这两条 JSON 路径。
	for _, want := range []string{
		`data-write-api="` + navCreateAPIPath + `"`,
		`data-write-api="` + navDeleteAPIPath + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页缺少 JSON 写声明 %q", want)
		}
	}
	if strings.Contains(body, "/admin/navigations/bulk-delete") {
		t.Error("列表页仍在引用已下线的页面批量删除路由")
	}
}

// TestNavigationDeleteBulkReasonsFollowRequestLanguage 逐条原因按请求语言出成品句（真实链路）。
//
// 词条用**合成值**注入（process 级缓存，退出还原）：响应里出现这句只能来自「enums 的 key 真的
// 走到了取词层」。两类失败各验一遍：
//   - 业务失败：合法但不存在（或已被本批删掉）的 uuid → ErrNotFound；
//   - 未归口的内部失败：非 uuid 的 id 会在库层报 22P02（uuid 解析失败）→ 归口 navigation.err.internal，
//     数据库原文只进日志。
//
// 语言走 Accept-Language：同一条请求换语言必须换文案，否则「按请求语言渲染」只是一句声明。
func TestNavigationDeleteBulkReasonsFollowRequestLanguage(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	i18n.InjectForTest(map[string]map[string]string{
		navigationenums.ErrNotFound: {"zh-CN": "合成-导航项不存在", "en-US": "synth-navigation-item-missing"},
		navInternalKey: {"zh-CN": "合成-操作失败", "en-US": "synth-operation-failed"},
	}, map[string]int{})
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 非 uuid：库层解析 uuid 失败（22P02）→ service 原样上抛 → 归口到 navigation.err.internal。
	const badID = "not-a-uuid"
	// 合法但不存在：Absent → ErrNotFound。
	const missing = "00000000-0000-0000-0000-0000000000fd"

	for _, tc := range []struct{ name, lang, wantMissing, wantInternal string }{
		{"中文", "zh-CN", "合成-导航项不存在", "合成-操作失败"},
		{"英文", "en-US", "synth-navigation-item-missing", "synth-operation-failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gone := addNavItem(t, env.NavSvc, env.ProjectID, "逐条原因-"+tc.name, "/reason-"+tc.lang, nil, "self")

			recorder := navWriteAuthzPostLang(t, env, env.Super, navDeleteAPIPath,
				map[string]interface{}{"ids": []string{gone, missing, badID}}, tc.lang)
			if recorder.Code != http.StatusOK {
				t.Fatalf("批档应 200（逐条结果在 data 里），实际 %d: %s", recorder.Code, recorder.Body.String())
			}
			body := map[string]interface{}{}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("回执不是 JSON: %s", recorder.Body.String())
			}
			data, _ := body["data"].(map[string]interface{})
			if data == nil {
				t.Fatalf("回执缺少 data 段: %s", recorder.Body.String())
			}
			if data["deleted"] != float64(1) || data["skipped"] != float64(2) {
				t.Fatalf("回执应为 deleted=1 skipped=2，实际 %s", recorder.Body.String())
			}
			items, _ := data["items"].([]interface{})
			for _, want := range []struct {
				id, message, why string
			}{
				{missing, tc.wantMissing, "业务失败（id 不存在）"},
				{badID, tc.wantInternal, "内部失败（非 uuid）"},
			} {
				item := navFindBulkItem(t, items, want.id)
				if item["deleted"] != false {
					t.Fatalf("%s 不该被标记为已删除: %v", want.why, item)
				}
				message, _ := item["message"].(string)
				if message != want.message {
					t.Fatalf("%s 在 %s 下的逐条原因应为 %q，实际 %q", want.why, tc.lang, want.message, message)
				}
				navAssertNoLeak(t, message)
			}
			if navItemExists(t, env, gone) {
				t.Fatalf("成功那一条应真的从库里消失（id=%s）", gone)
			}
		})
	}
}
