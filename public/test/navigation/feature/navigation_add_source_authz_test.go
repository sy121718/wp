package feature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func navCountItems(t *testing.T, env *navTranslationAuthzEnv) int {
	t.Helper()
	items, err := env.NavSvc.List(context.Background(),
		&navigationdto.ListReq{ProjectID: env.ProjectID, Kind: "header"})
	if err != nil {
		t.Fatalf("列导航项失败: %v", err)
	}
	return len(items)
}

func navAddSourceSuccess(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `role="status"`) || strings.Contains(body, "data-ui-error") {
		t.Fatalf("成功应走公共提示组件，实际：%s", body)
	}
	return body
}

func TestNavigationAddSourceWriteAuthzChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})
	navWriteAuthzSetPolicy(t, env, navSourceAPIPath, navSourcePermission, false)
	if got := navWriteAuthzPolicyCount(t, env, navSourceAPIPath, navSourcePermission); got != 0 {
		t.Fatalf("反例前提不成立：普通管理员仍有 %d 条 %s 策略", got, navSourcePermission)
	}

	before := navCountItems(t, env)
	denied := navWriteAuthzPost(t, env, env.Plain, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageThree), true)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员应 403，实际 %d: %s", denied.Code, denied.Body.String())
	}
	if got := navCountItems(t, env); got != before {
		t.Fatalf("403 的请求不该落库：条数 %d → %d", before, got)
	}

	navWriteAuthzSetPolicy(t, env, navSourceAPIPath, navSourcePermission, true)
	allowed := navWriteAuthzPost(t, env, env.Plain, navSourceAPIPath,
		navAddSourcePayload(env, "article", navSrcArticle), true)
	navAddSourceSuccess(t, allowed)
	created := navItemByTitle(t, env, "你好世界")
	if created == "" {
		t.Fatalf("加入成功但行没落库：%s", allowed.Body.String())
	}
	bound := navItemSourceBinding(t, env, created)
	if bound.sourceType != "article" || bound.sourceID != navSrcArticle {
		t.Fatalf("来源绑定应写进行（之后构建期据此同步标题/路径），实际 %+v", bound)
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

// TestNavigationAddSourceMixedBatchRejectedWhole 覆盖整批语义：
// 合法项与「无公开路径」项混合时，整批拒绝、合法项也不落库。
func TestNavigationAddSourceMixedBatchRejectedWhole(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})
	i18n.InjectForTest(map[string]map[string]string{
		navigationenums.ErrBatchReject: {"zh-CN": "合成-整批拒绝", "en-US": "synth-batch-rejected"},
	}, map[string]int{})
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	before := navCountItems(t, env)
	recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageTwo), true)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("含无公开路径项应整批 400，实际 %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("整批拒绝应渲染共享错误片段：%s", body)
	}
	if strings.Contains(body, "<ul>") || strings.Contains(body, navSrcPageOne) {
		t.Fatalf("失败区不逐条列明细（明细只进 data.items）：%s", body)
	}
	if got := navCountItems(t, env); got != before {
		t.Fatalf("整批拒绝不该写入任何行：条数 %d → %d", before, got)
	}
}

// TestNavigationAddSourceMissingCandidateIsNotFound 覆盖候选缺失：状态按 404 声明。
func TestNavigationAddSourceMissingCandidateIsNotFound(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})
	before := navCountItems(t, env)
	recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne, "00000000-0000-0000-0000-0000000000aa"), true)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("候选不存在应 404，实际 %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := navCountItems(t, env); got != before {
		t.Fatalf("整批拒绝不该写入任何行：条数 %d → %d", before, got)
	}
}

// TestNavigationAddSourcePathConflictRejectsWholeBatch 覆盖路径冲突：整批拒绝且零写入。
func TestNavigationAddSourcePathConflictRejectsWholeBatch(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	env.NavSvc.SetSourceResolver(navSourceStub{})
	before := navCountItems(t, env)
	recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageThree), true)
	navAddSourceSuccess(t, recorder)
	if got := navCountItems(t, env); got != before+2 {
		t.Fatalf("合法整批应写入 2 行，实际 %d → %d", before, got)
	}

	// 同一候选再次加入时路径已存在：整批拒绝，不追加第二份。
	again := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
		navAddSourcePayload(env, "page", navSrcPageOne), true)
	if again.Code < 400 {
		t.Fatalf("路径已存在的重复加入应整批拒绝，实际 %d: %s", again.Code, again.Body.String())
	}
	if got := navCountItems(t, env); got != before+2 {
		t.Fatalf("被拒绝的重复加入不该写入：条数 %d", got)
	}
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
	navAddSourceSuccess(t, withToken)
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
		`hx-post="` + navSourceAPIPath + `"`,
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
		navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageThree), true)
	navAddSourceSuccess(t, recorder)

	menus, items, menuKeys, itemKeys := spy.counts()
	// 菜单级失效按 kind 合并成一次；每个新建项各自派发一次 navigation 失效。
	if menus != 1 || items != 2 {
		t.Fatalf("整批应派发 menu=1 navigation=2，实际 menu=%d navigation=%d",
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
	t.Run("整批拒绝", func(t *testing.T) {
		env := newNavTranslationAuthzEnv(t)
		env.NavSvc.SetSourceResolver(navSourceStub{})
		spy := &navStaleSpy{}
		navStaleSpyAttach(t, env, spy)

		recorder := navWriteAuthzPost(t, env, env.Super, navSourceAPIPath,
			navAddSourcePayload(env, "page", navSrcPageOne, navSrcPageTwo), true)
		if recorder.Code < 400 {
			t.Fatalf("含无公开路径项应整批拒绝，实际 %d: %s", recorder.Code, recorder.Body.String())
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
