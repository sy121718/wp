package feature

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"regexp"
	"strings"
	"testing"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationservice "go_wp/internal/module/navigation/service"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/public/test/support"
)

type navSourceStub struct{}

const (
	navSrcPageOne   = "11111111-1111-1111-1111-111111111111"
	navSrcPageTwo   = "22222222-2222-2222-2222-222222222222"
	navSrcPageThree = "44444444-4444-4444-4444-444444444444"
	navSrcArticle   = "33333333-3333-3333-3333-333333333333"
)

func (navSourceStub) Candidates(_ context.Context, _ string) ([]navigationcontract.SourceGroup, error) {
	return []navigationcontract.SourceGroup{
		{Type: "page", Items: []navigationcontract.SourceCandidate{
			{ID: navSrcPageOne, Label: "/about", Title: "关于我们", URL: "/about"},
			{ID: navSrcPageTwo, Label: "/draft", Title: "草稿页", URL: ""},
			{ID: navSrcPageThree, Label: "/contact", Title: "联系我们", URL: "/contact"},
		}},
		{Type: "article", Items: []navigationcontract.SourceCandidate{
			{ID: navSrcArticle, Label: "hello", Title: "你好世界", URL: "/blog/hello"},
		}},
	}, nil
}

func (navSourceStub) ResolveSource(_ context.Context, _, _, _ string) (string, string, error) {
	return "", "", nil
}

func navAddSourcePayload(env *navTranslationAuthzEnv, sourceType string, ids ...string) map[string]interface{} {
	return map[string]interface{}{
		"projectId": env.ProjectID, "kind": "header",
		"sourceType": sourceType, "ids": ids,
	}
}

// navAddSourceData 从 Jet 结果片段读回计数，形状与旧 JSON 的 data 段一致，
// 调用点的断言不用改。
func navAddSourceData(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	m := regexp.MustCompile(`data-added="(\d+)" data-skipped="(\d+)"`).FindSubmatch(body)
	if m == nil {
		t.Fatalf("回执缺少计数属性: %s", string(body))
	}
	added, _ := strconv.Atoi(string(m[1]))
	skipped, _ := strconv.Atoi(string(m[2]))

	// 逐条事实同样来自片段：<li data-item-id=".." data-item-ok="0|1">原因</li>
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
			"added":   string(im[2]) == "1",
			"message": message,
		})
	}
	return map[string]interface{}{"added": float64(added), "skipped": float64(skipped), "items": items}
}

func navCountItems(t *testing.T, env *navTranslationAuthzEnv) int {
	t.Helper()
	items, err := env.NavSvc.List(context.Background(),
		&navigationdto.ListReq{ProjectID: env.ProjectID, Kind: "header"})
	if err != nil {
		t.Fatalf("列导航项失败: %v", err)
	}
	return len(items)
}

func TestNavigationAddSourceWriteAuthzChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})
	navWriteAuthzSetPolicy(t, env, navSourceAPIPath, navSourcePermission, false)
	if got := navWriteAuthzPolicyCount(t, env, navSourceAPIPath, navSourcePermission); got != 0 {
		t.Fatalf("反例前提不成立：普通管理员仍有 %d 条 %s 策略", got, navCreatePermission)
	}

	before := navCountItems(t, env)
	denied := navWriteAuthzPost(t, env, env.Plain, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageTwo), true)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员应 403，实际 %d: %s", denied.Code, denied.Body.String())
	}
	if got := navCountItems(t, env); got != before {
		t.Fatalf("403 的请求不该落库：条数 %d → %d", before, got)
	}

	navWriteAuthzSetPolicy(t, env, navSourceAPIPath, navSourcePermission, true)
	allowed := navWriteAuthzPost(t, env, env.Plain, navSourceAPIPath,
		navAddSourcePayload(env, "article", navSrcArticle), true)
	if allowed.Code != http.StatusOK {
		t.Fatalf("补上策略后应 200（说明 403 由该条 Casbin 路径决定），实际 %d: %s",
			allowed.Code, allowed.Body.String())
	}
	data := navAddSourceData(t, allowed.Body.Bytes())
	if data["added"] != float64(1) || data["skipped"] != float64(0) {
		t.Fatalf("应 added=1 skipped=0，实际 %s", allowed.Body.String())
	}
	created := navItemByTitle(t, env, "你好世界")
	if created == "" {
		t.Fatalf("加入成功但行没落库：%s", allowed.Body.String())
	}
	bound := navItemSourceBinding(t, env, created)
	if bound.sourceType != "article" || bound.sourceID != navSrcArticle {
		t.Fatalf("来源绑定应写进行（之后构建期据此同步标题/路径），实际 %+v", bound)
	}

	mixed := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageTwo, navSrcArticle), true)
	if mixed.Code != http.StatusOK {
		t.Fatalf("超管应 200，实际 %d: %s", mixed.Code, mixed.Body.String())
	}
	mixedData := navAddSourceData(t, mixed.Body.Bytes())
	if mixedData["added"] != float64(1) || mixedData["skipped"] != float64(2) {
		t.Fatalf("应 added=1 skipped=2，实际 %s", mixed.Body.String())
	}
	items, _ := mixedData["items"].([]interface{})
	if len(items) != 3 {
		t.Fatalf("逐条结果应有 3 条，实际 %d 条：%s", len(items), mixed.Body.String())
	}
	for _, raw := range items[1:] {
		item, _ := raw.(map[string]interface{})
		message, _ := item["message"].(string)
		if item["added"] != false || message == "" {
			t.Fatalf("跳过项必须带原因：%v", item)
		}
		for _, token := range []string{"SQLSTATE", "pg_", "constraint", "record not found"} {
			if strings.Contains(message, token) {
				t.Errorf("逐条原因泄漏内部细节 %q：%q", token, message)
			}
		}
	}
	if navItemByTitle(t, env, "关于我们") == "" {
		t.Fatal("有公开路径的候选应真的加入菜单")
	}

	again := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne), true)
	if again.Code != http.StatusOK {
		t.Fatalf("重复加入应 200（逐条原因在 data 里），实际 %d: %s", again.Code, again.Body.String())
	}
	againData := navAddSourceData(t, again.Body.Bytes())
	if againData["added"] != float64(0) || againData["skipped"] != float64(1) {
		t.Fatalf("重复加入应 added=0 skipped=1，实际 %s", again.Body.String())
	}
}

func TestNavigationAddSourceReasonsFallBackWhenEntryMissing(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})
	// 词条缺席时，逐条原因必须仍是受控句：不许裸 key，也不许内部原文。
	i18n.InjectForTest(nil, map[string]int{})
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageTwo, navSrcArticle), true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("应 200（逐条结果在 data 里），实际 %d: %s", recorder.Code, recorder.Body.String())
	}
	data := navAddSourceData(t, recorder.Body.Bytes())
	if data["added"] != float64(1) || data["skipped"] != float64(2) {
		t.Fatalf("应 added=1 skipped=2，实际 %s", recorder.Body.String())
	}
	items, _ := data["items"].([]interface{})
	for _, want := range []struct{ id, key, why string }{
		{navSrcPageTwo, "admin.navigations.no_public_path", "候选暂无公开路径（正常跳过）"},
		{navSrcArticle, navigationenums.ErrNotFound, "候选不属于本工程的该来源类型"},
	} {
		item := navFindBulkItem(t, items, want.id)
		if item["added"] != false {
			t.Fatalf("%s 不该被标记为已加入: %v", want.why, item)
		}
		message, _ := item["message"].(string)
		if strings.TrimSpace(message) == "" {
			t.Fatalf("%s 的逐条原因不能为空", want.why)
		}
		if message == want.key {
			t.Fatalf("%s 的逐条原因不该是裸 key：%q", want.why, message)
		}
		navAssertNoLeak(t, message)
	}
}

func TestNavigationAddSourceReasonsFollowRequestLanguageChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})

	const noPathKey = "admin.navigations.no_public_path"
	i18n.InjectForTest(map[string]map[string]string{
		noPathKey:                   {"zh-CN": "合成-暂无公开路径", "en-US": "synth-no-public-path"},
		navigationenums.ErrNotFound: {"zh-CN": "合成-不存在", "en-US": "synth-not-found"},
	}, map[string]int{})
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	for _, tc := range []struct{ name, lang, wantNoPath, wantNotFound string }{
		{"中文", "zh-CN", "合成-暂无公开路径", "合成-不存在"},
		{"英文", "en-US", "synth-no-public-path", "synth-not-found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := navWriteAuthzPostLang(t, env, env.Super, navSourceAPIPath,
				navAddSourcePayload(env, "page", navSrcPageTwo, navSrcArticle), tc.lang)
			if recorder.Code != http.StatusOK {
				t.Fatalf("应 200，实际 %d: %s", recorder.Code, recorder.Body.String())
			}
			data := navAddSourceData(t, recorder.Body.Bytes())
			if data["added"] != float64(0) || data["skipped"] != float64(2) {
				t.Fatalf("应 added=0 skipped=2，实际 %s", recorder.Body.String())
			}
			items, _ := data["items"].([]interface{})
			for _, want := range []struct{ id, message, why string }{
				{navSrcPageTwo, tc.wantNoPath, "暂无公开路径"},
				{navSrcArticle, tc.wantNotFound, "不属于本来源类型"},
			} {
				item := navFindBulkItem(t, items, want.id)
				if message, _ := item["message"].(string); message != want.message {
					t.Fatalf("%s 在 %s 下的逐条原因应为 %q，实际 %q", want.why, tc.lang, want.message, message)
				}
			}
		})
	}
}

func navItemSourceBinding(t *testing.T, env *navTranslationAuthzEnv, id string) struct{ sourceType, sourceID string } {
	t.Helper()
	item, err := env.NavSvc.Get(context.Background(), &navigationdto.GetReq{ID: id})
	if err != nil || item == nil {
		t.Fatalf("读导航项失败（id=%s）: %v", id, err)
	}
	out := struct{ sourceType, sourceID string }{sourceType: item.SourceType}
	if item.SourceID != nil {
		out.sourceID = *item.SourceID
	}
	return out
}

func TestNavigationAddSourceRequiresCSRF(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})

	before := navCountItems(t, env)
	withoutToken := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne), false)
	if withoutToken.Code != http.StatusForbidden {
		t.Fatalf("缺少 X-CSRF-Token 的写请求应 403，实际 %d: %s", withoutToken.Code, withoutToken.Body.String())
	}
	if got := navCountItems(t, env); got != before {
		t.Fatalf("被 CSRF 拦下的请求不该落库：条数 %d → %d", before, got)
	}

	withToken := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne), true)
	if withToken.Code != http.StatusOK {
		t.Fatalf("补上令牌的同一请求应 200，实际 %d: %s", withToken.Code, withToken.Body.String())
	}
	if navItemByTitle(t, env, "关于我们") == "" {
		t.Fatal("补令牌后应真的加入菜单")
	}
}

func TestNavigationAddSourceOverLimitIsRejected(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, "src-"+strconv.Itoa(i))
	}
	before := navCountItems(t, env)
	recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath, navAddSourcePayload(env, "page", ids...), true)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("超限应 400，实际 %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Fatalf("超限应回错误片段：%s", body)
	}
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) {
		t.Fatalf("超限提示应带上限 %d：%s", bulkids.MaxIDs, body)
	}
	if got := navCountItems(t, env); got != before {
		t.Fatalf("超限不该写库：条数 %d → %d", before, got)
	}
}

func TestNavigationAddSourceLegacyPagePostIsGone(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodPost,
		Path:   "/admin/navigations/add-source",
		Headers: map[string]string{
			"Cookie":       env.Super.Cookie,
			"X-CSRF-Token": env.Super.CSRFToken,
			"Content-Type": "application/x-www-form-urlencoded",
		},
		RawBody: []byte("projectId=" + env.ProjectID + "&kind=header&sourceType=page&sourceIds=" + navSrcPageOne),
	})
	if err != nil {
		t.Fatalf("发送旧页面 POST 失败: %v", err)
	}
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("旧页面写路由应 404，实际 %d: %s", recorder.Code, recorder.Body.String())
	}

	env.NavSvc.SetSourceResolver(navSourceStub{})
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
		`name="sourceType"`,
		`data-write-result`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("来源抽屉缺少 HTMX 写声明 %q", want)
		}
	}
	if strings.Contains(body, "/admin/navigations/add-source") {
		t.Error("列表页仍在引用已下线的页面来源加入路由")
	}
}

type navStaleSpy struct {
	c navStaleCounter
}

func (s *navStaleSpy) counts() (int, int, []string, []string) {
	menu, nav := s.c.counts()
	menus, items := s.c.snapshot()
	return menu, nav, menus, items
}

func navStaleSpyAttach(t *testing.T, env *navTranslationAuthzEnv, spy *navStaleSpy) {
	t.Helper()
	if env.Ports.Pages == nil || env.Ports.Items == nil {
		t.Fatalf("env.Ports 未装配：计数装饰器需要真实端口做委派")
	}

	setter, ok := env.NavSvc.(interface {
		SetWritePorts(navigationservice.NavigationWriteDeps)
	})
	if !ok {
		t.Fatalf("NavSvc 未暴露 SetWritePorts：端口注入点变了，本用例失去计数能力")
	}
	setter.SetWritePorts(navStaleCountDeps(env.Ports, &spy.c))
}

type navSourceErrStub struct{}

func (navSourceErrStub) Candidates(_ context.Context, _ string) ([]navigationcontract.SourceGroup, error) {
	return nil, errors.New(`relation "pages" does not exist`)
}

func (navSourceErrStub) ResolveSource(_ context.Context, _, _, _ string) (string, string, error) {
	return "", "", nil
}

func TestNavigationAddSourceDispatchesStalePerCreatedItem(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})
	spy := &navStaleSpy{}
	navStaleSpyAttach(t, env, spy)

	recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageThree, navSrcPageTwo, navSrcArticle), true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d: %s", recorder.Code, recorder.Body.String())
	}
	data := navAddSourceData(t, recorder.Body.Bytes())
	if data["added"] != float64(2) || data["skipped"] != float64(2) {
		t.Fatalf("应 added=2 skipped=2，实际 %s", recorder.Body.String())
	}

	menus, items, menuKeys, itemKeys := spy.counts()
	if menus != 2 || items != 2 {
		t.Fatalf("两条成功应各派发一次：menu=2 navigation=2，实际 menu=%d navigation=%d（跳过项不许派发）",
			menus, items)
	}
	for _, key := range menuKeys {
		if key != env.ProjectID+"|header" {
			t.Errorf("menu 键应为 {project}|{kind}，实际 %q", key)
		}
	}

	created := map[string]bool{}
	for _, title := range []string{"关于我们", "联系我们"} {
		id := navItemByTitle(t, env, title)
		if id == "" {
			t.Fatalf("成功的候选应真的落库：%q 不在列表里", title)
		}
		created[env.ProjectID+"|"+id] = true
	}
	for _, key := range itemKeys {
		if !created[key] {
			t.Errorf("navigation 键应指向本次新建的行，实际 %q（已建：%v）", key, created)
		}
	}
}

func TestNavigationAddSourceNoDispatchWhenNothingWritten(t *testing.T) {
	t.Run("全部跳过", func(t *testing.T) {
		env := newNavTranslationAuthzEnv(t)
		env.NavSvc.SetSourceResolver(navSourceStub{})
		spy := &navStaleSpy{}
		navStaleSpyAttach(t, env, spy)

		recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
			navAddSourcePayload(env, "page", navSrcPageTwo, navSrcArticle), true)
		if recorder.Code != http.StatusOK {
			t.Fatalf("应 200（逐条原因在 data 里），实际 %d: %s", recorder.Code, recorder.Body.String())
		}
		data := navAddSourceData(t, recorder.Body.Bytes())
		if data["added"] != float64(0) || data["skipped"] != float64(2) {
			t.Fatalf("应 added=0 skipped=2，实际 %s", recorder.Body.String())
		}
		if menus, items, _, _ := spy.counts(); menus != 0 || items != 0 {
			t.Fatalf("没有写入就不许派发失效，实际 menu=%d navigation=%d", menus, items)
		}
	})

	t.Run("读候选失败", func(t *testing.T) {
		env := newNavTranslationAuthzEnv(t)
		env.NavSvc.SetSourceResolver(navSourceErrStub{})
		spy := &navStaleSpy{}
		navStaleSpyAttach(t, env, spy)

		recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
			navAddSourcePayload(env, "page", navSrcPageOne), true)
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("读候选失败应 500，实际 %d: %s", recorder.Code, recorder.Body.String())
		}
		if menus, items, _, _ := spy.counts(); menus != 0 || items != 0 {
			t.Fatalf("失败档必须零派发，实际 menu=%d navigation=%d", menus, items)
		}
	})
}
