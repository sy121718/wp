package feature

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/i18n"
	"go_wp/public/test/support"
)

const (
	navCreateAPIPath = "/api/navigation/create"
	navSourceAPIPath = "/api/navigation/sources"
	navDeleteAPIPath     = "/api/navigation/delete"
	navDeleteBulkAPIPath = "/api/navigation/delete-bulk"

	navCreatePermission = "navigation:create"
	navSourcePermission = "navigation:sourceAdd"
	navDeletePermission     = "navigation:delete"
	navDeleteBulkPermission = "navigation:deleteBulk"
)

// navPayloadForm 把测试用的 map 转成标准表单（ids 展开成重复字段）。
// 请求里若还带 action，说明是旧协议残留，直接忽略它。
func navPayloadForm(payload map[string]interface{}) url.Values {
	form := url.Values{}
	for k, v := range payload {
		if k == "action" {
			continue
		}
		switch vals := v.(type) {
		case []string:
			for _, item := range vals {
				form.Add(k, item)
			}
		case string:
			form.Set(k, vals)
		default:
			form.Set(k, fmt.Sprint(vals))
		}
	}
	return form
}

func navWriteAuthzPost(t *testing.T, env *navTranslationAuthzEnv, sess *support.AdminSession,
	path string, payload map[string]interface{}, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	raw := []byte(navPayloadForm(payload).Encode())
	headers := map[string]string{
		"Cookie":       sess.Cookie,
		"Content-Type": "application/x-www-form-urlencoded",
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

func navWriteAuthzPostLang(t *testing.T, env *navTranslationAuthzEnv, sess *support.AdminSession,
	path string, payload map[string]interface{}, lang string) *httptest.ResponseRecorder {
	t.Helper()
	raw := []byte(navPayloadForm(payload).Encode())
	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodPost, Path: path,
		Headers: map[string]string{
			"Cookie":          sess.Cookie,
			"X-CSRF-Token":    sess.CSRFToken,
			"Content-Type":    "application/x-www-form-urlencoded",
			"Accept-Language": lang,
		},
		RawBody: raw,
	})
	if err != nil {
		t.Fatalf("发送 POST %s 失败: %v", path, err)
	}
	return recorder
}

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

// navBulkDeleteData 从结果片段读回 deleted / skipped / items，形状与旧 JSON 的 data 段一致。
func navBulkDeleteData(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	m := regexp.MustCompile(`data-deleted="(\d+)" data-skipped="(\d+)"`).FindSubmatch(body)
	if m == nil {
		t.Fatalf("回执缺少计数属性: %s", string(body))
	}
	deleted, _ := strconv.Atoi(string(m[1]))
	skipped, _ := strconv.Atoi(string(m[2]))
	items := make([]interface{}, 0, 4)
	for _, im := range regexp.MustCompile(`data-item-id="([^"]*)" data-item-ok="([01])">([^<]*)<`).FindAllSubmatch(body, -1) {
		message := ""
		if text := string(im[3]); text != "" {
			if i := strings.Index(text, "："); i >= 0 {
				message = text[i+len("："):]
			}
		}
		items = append(items, map[string]interface{}{
			"id":      string(im[1]),
			"deleted": string(im[2]) == "1",
			"message": message,
		})
	}
	return map[string]interface{}{"deleted": float64(deleted), "skipped": float64(skipped), "items": items}
}

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

func navAssertNoLeak(t *testing.T, message string) {
	t.Helper()
	for _, tok := range []string{"SQLSTATE", "pg_", "constraint", "record not found", navInternalKey} {
		if strings.Contains(message, tok) {
			t.Errorf("逐条原因泄漏内部细节 %q：%q", tok, message)
		}
	}
}

func navItemExists(t *testing.T, env *navTranslationAuthzEnv, id string) bool {
	t.Helper()
	item, err := env.NavSvc.Get(context.Background(), &navigationdto.GetReq{ID: id})
	if err != nil || item == nil {
		return false
	}
	return item.ID == id
}

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

func TestNavigationDeleteWriteAuthzChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	navWriteAuthzSetPolicy(t, env, navDeleteBulkAPIPath, navDeleteBulkPermission, false)
	if got := navWriteAuthzPolicyCount(t, env, navDeleteBulkAPIPath, navDeleteBulkPermission); got != 0 {
		t.Fatalf("反例前提不成立：普通管理员仍有 %d 条 %s 策略", got, navDeletePermission)
	}

	victim := addNavItem(t, env.NavSvc, env.ProjectID, "待删项", "/victim", nil, "self")

	denied := navWriteAuthzPost(t, env, env.Plain, navDeleteAPIPath,
		map[string]interface{}{"id": victim}, true)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员应 403，实际 %d: %s", denied.Code, denied.Body.String())
	}

	if !navItemExists(t, env, victim) {
		t.Fatalf("403 的请求不该真删掉行（id=%s）", victim)
	}

	asAdmin := navWriteAuthzPost(t, env, env.Super, navDeleteAPIPath,
		map[string]interface{}{"id": victim}, true)
	if asAdmin.Code != http.StatusOK {
		t.Fatalf("超管删除应 200，实际 %d: %s", asAdmin.Code, asAdmin.Body.String())
	}
	if navItemExists(t, env, victim) {
		t.Fatalf("超管删除后行仍存在（id=%s）", victim)
	}

	batchA := addNavItem(t, env.NavSvc, env.ProjectID, "批删 A", "/batch-a", nil, "self")
	batchB := addNavItem(t, env.NavSvc, env.ProjectID, "批删 B", "/batch-b", nil, "self")
	deniedBulk := navWriteAuthzPost(t, env, env.Plain, navDeleteBulkAPIPath,
		map[string]interface{}{"ids": []string{batchA, batchB}}, true)
	if deniedBulk.Code != http.StatusForbidden {
		t.Fatalf("无策略的批量删除应 403，实际 %d: %s", deniedBulk.Code, deniedBulk.Body.String())
	}
	if !navItemExists(t, env, batchA) || !navItemExists(t, env, batchB) {
		t.Fatal("403 的批量删除不该删掉任何一行")
	}

	navWriteAuthzSetPolicy(t, env, navDeleteBulkAPIPath, navDeleteBulkPermission, true)
	allowed := navWriteAuthzPost(t, env, env.Plain, navDeleteBulkAPIPath,
		map[string]interface{}{"ids": []string{batchA, batchB}}, true)
	if allowed.Code != http.StatusOK {
		t.Fatalf("补上策略后普通管理员应 200（说明 403 由该条 Casbin 路径决定），实际 %d: %s",
			allowed.Code, allowed.Body.String())
	}
	data := navBulkDeleteData(t, allowed.Body.Bytes())
	if data["deleted"] != float64(2) || data["skipped"] != float64(0) {
		t.Fatalf("批量回执应为 deleted=2 skipped=0，实际 %s", allowed.Body.String())
	}
	if navItemExists(t, env, batchA) || navItemExists(t, env, batchB) {
		t.Fatal("补策略后的批量删除应真的删掉两行")
	}
}

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

func TestNavigationWriteRequiresCSRF(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	victim := addNavItem(t, env.NavSvc, env.ProjectID, "CSRF 待删项", "/csrf-victim", nil, "self")

	withoutToken := navWriteAuthzPost(t, env, env.Super, navDeleteAPIPath,
		map[string]interface{}{"id": victim}, false)

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

func TestNavigationDeleteBulkResultFromRealRows(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	goneA := addNavItem(t, env.NavSvc, env.ProjectID, "批删甲", "/bulk-gone-a", nil, "self")
	goneB := addNavItem(t, env.NavSvc, env.ProjectID, "批删乙", "/bulk-gone-b", nil, "self")
	kept := addNavItem(t, env.NavSvc, env.ProjectID, "不删项", "/bulk-keep", nil, "self")

	const missing = "00000000-0000-0000-0000-0000000000ff"
	recorder := navWriteAuthzPost(t, env, env.Super, navDeleteBulkAPIPath,
		map[string]interface{}{"ids": []string{goneA, missing, goneB}}, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("批档应 200（逐条结果在 data 里），实际 %d: %s", recorder.Code, recorder.Body.String())
	}

	data := navBulkDeleteData(t, recorder.Body.Bytes())
	if data["deleted"] != float64(2) || data["skipped"] != float64(1) {
		t.Fatalf("回执应为 deleted=2 skipped=1，实际 %s", recorder.Body.String())
	}
	items, _ := data["items"].([]interface{})
	if len(items) != 3 {
		t.Fatalf("逐条结果应有 3 条（含失败那条），实际 %d 条: %s", len(items), recorder.Body.String())
	}

	failed := navFindBulkItem(t, items, missing)
	if failed["deleted"] != false {
		t.Fatalf("不存在的 id 不该被标记为已删除: %v", failed)
	}
	// 词条缺席时逐条原因仍是受控句：不许裸 key，也不许内部原文。
	message, _ := failed["message"].(string)
	if strings.TrimSpace(message) == "" || message == navigationenums.ErrNotFound {
		t.Fatalf("逐条原因应为受控句，实际 %q", message)
	}
	navAssertNoLeak(t, message)

	if navItemExists(t, env, goneA) || navItemExists(t, env, goneB) {
		t.Fatal("成功那两条应真的从库里消失")
	}
	if !navItemExists(t, env, kept) {
		t.Fatal("没参与本次批量的行不该被删")
	}
}

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

	for _, want := range []string{
		`hx-post="` + navCreateAPIPath + `"`,
		`hx-post="` + navDeleteAPIPath + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表页缺少 HTMX 写声明 %q", want)
		}
	}
	if strings.Contains(body, "/admin/navigations/bulk-delete") {
		t.Error("列表页仍在引用已下线的页面批量删除路由")
	}
}

func TestNavigationDeleteBulkReasonsFollowRequestLanguage(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	i18n.InjectForTest(map[string]map[string]string{
		navigationenums.ErrNotFound: {"zh-CN": "合成-导航项不存在", "en-US": "synth-navigation-item-missing"},
		navInternalKey:              {"zh-CN": "合成-操作失败", "en-US": "synth-operation-failed"},
	}, map[string]int{})
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	const badID = "not-a-uuid"

	const missing = "00000000-0000-0000-0000-0000000000fd"

	for _, tc := range []struct{ name, lang, wantMissing, wantInternal string }{
		{"中文", "zh-CN", "合成-导航项不存在", "合成-操作失败"},
		{"英文", "en-US", "synth-navigation-item-missing", "synth-operation-failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gone := addNavItem(t, env.NavSvc, env.ProjectID, "逐条原因-"+tc.name, "/reason-"+tc.lang, nil, "self")

			recorder := navWriteAuthzPostLang(t, env, env.Super, navDeleteBulkAPIPath,
				map[string]interface{}{"ids": []string{gone, missing, badID}}, tc.lang)
			if recorder.Code != http.StatusOK {
				t.Fatalf("批档应 200（逐条结果在 data 里），实际 %d: %s", recorder.Code, recorder.Body.String())
			}
			data := navBulkDeleteData(t, recorder.Body.Bytes())
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
