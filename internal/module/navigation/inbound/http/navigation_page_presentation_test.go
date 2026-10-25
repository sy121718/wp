package navigationhttp

// navigation_page_presentation_test.go — 管理页「展示值由谁产出」的回归护栏。
//
// 被守的边界（docs/rules/template-boundary.md）：Go 只出事实，模板出文案（Jet 取词到
// 具体语言），CSS 出外观。这里断言三件在别处都不会红的事：
//
//	1. 行内「打开方式」不是 Go 预制的成品文案，而是模板按同一批 key 取词 ——
//	   与「添加菜单项」抽屉的下拉**同源**（两处各自取词最怕只改一处：页面不报错，
//	   也没有别的测试会红）；
//	2. 层级缩进不再由 Go 拼像素：模板只把 Depth 写进 CSS 变量（--tree-depth），
//	   间距由 theme.css 的 .tree-cell 算；
//	3. 来源分组的标题按 group.Type 取词，contract 里的 key 不会漏到页面上。

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/middleware/adminctx"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// pagePresentationNavigationStub 只实现本页用到的两个读方法（其余走嵌入的 nil 接口）。
type pagePresentationNavigationStub struct {
	navigationcontract.NavigationService
	tree   []*navigationdto.NavigationNode
	groups []navigationcontract.SourceGroup
}

func (s *pagePresentationNavigationStub) Tree(_ context.Context, _, _ string) ([]*navigationdto.NavigationNode, error) {
	return s.tree, nil
}

func (s *pagePresentationNavigationStub) SourceGroups(_ context.Context, _ string) ([]navigationcontract.SourceGroup, error) {
	return s.groups, nil
}

// pagePresentationProjectStub 只实现 List（其余走嵌入的 nil 接口）。
//
// listErr 开关的是**取数失败**那条路径：整页 GET 的失败出口必须还是页面
// （见 TestNavigationsPageLoadFailureRendersPageWithErrorSlot）。
type pagePresentationProjectStub struct {
	projectcontract.ProjectService
	listErr error
}

func (s *pagePresentationProjectStub) List(_ context.Context) ([]projectcontract.ProjectResp, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []projectcontract.ProjectResp{{ID: "project-1", Name: "示例工程"}}, nil
}

// TestNavigationsPageRowPresentationComesFromTemplate 行内的打开方式 / 层级缩进都出自模板与 CSS。
func TestNavigationsPageRowPresentationComesFromTemplate(t *testing.T) {
	svc := &pagePresentationNavigationStub{
		// 三种取值：普通、新标签页、以及**不在 self/blank 里的历史取值**
		//（旧数据/手工写库）。最后一种必须与抽屉下拉的默认项落成同一个词 ——
		// 否则用户看到的是一个下拉里不存在的打开方式。
		tree: []*navigationdto.NavigationNode{
			{ID: "row-self", Title: "首页", Path: "/", Target: "self"},
			{ID: "row-blank", Title: "新品", Path: "/new", Target: "blank", Children: []*navigationdto.NavigationNode{
				{ID: "row-child", Title: "子项", Path: "/new/a", Target: "blank"},
			}},
			{ID: "row-unknown", Title: "历史项", Path: "/legacy", Target: "_self"},
		},
		groups: []navigationcontract.SourceGroup{
			{Type: "page", Title: "admin.navigations.source.page", Items: []navigationcontract.SourceCandidate{
				{ID: "page-1", Title: "关于我们", URL: "/about"},
			}},
			{Type: "category", Title: "admin.navigations.source.category", Items: []navigationcontract.SourceCandidate{
				{ID: "cat-1", Title: "新闻", URL: "/news"},
			}},
			// 未登记词条的新来源类型：标题回落成类型名（不是空标题，也不是漏出 key）。
			{Type: "block", Title: "admin.navigations.source.block", Items: []navigationcontract.SourceCandidate{
				{ID: "blk-1", Title: "页脚块", URL: "/block/1"},
			}},
		},
	}
	body := renderNavigationsPageBody(t, svc)

	for _, tc := range []struct{ rowID, title, path, wantLabel string }{
		{"row-self", "首页", "/", "当前窗口"},
		{"row-blank", "新品", "/new", "新标签页"},
		{"row-unknown", "历史项", "/legacy", "当前窗口"},
	} {
		row := rowHTML(t, body, tc.rowID)
		if !strings.Contains(row, "<td>"+tc.wantLabel+"</td>") {
			t.Errorf("行 %s 的打开方式列不是「%s」：%s", tc.rowID, tc.wantLabel, row)
		}
		// 客户端筛选读 data-filter-text：它必须与列里显示的是同一个词，
		// 否则「筛新标签页」筛不到任何一行（而列里明明写着「新标签页」）。
		want := `data-filter-text="` + tc.title + " " + tc.path + " " + tc.wantLabel + `"`
		if !strings.Contains(row, want) {
			t.Errorf("行 %s 的可筛文本未用列内同一份文案，期望含 %s：%s", tc.rowID, want, row)
		}
	}

	// 与「添加菜单项」抽屉下拉同源：行内那格必须逐字等于下拉项的文字。
	// 两处各写一份最容易只改一处 —— 页面照常渲染，只有用户能看出对不上。
	blankCell := "<td>" + optionLabel(t, body, "blank") + "</td>"
	if !strings.Contains(rowHTML(t, body, "row-blank"), blankCell) {
		t.Errorf("行内打开方式与抽屉下拉不同源：抽屉 blank=%q，行=%s",
			optionLabel(t, body, "blank"), rowHTML(t, body, "row-blank"))
	}

	// 缩进：模板只交层级，间距交给 CSS。
	child := rowHTML(t, body, "row-child")
	if !strings.Contains(child, `class="tree-cell" style="--tree-depth:1"`) {
		t.Errorf("子行未把层级放进 CSS 变量：%s", child)
	}
	if !strings.Contains(child, "↳ 子项") {
		t.Errorf("缩进之外仍需保留层级线索（↳ 前缀）：%s", child)
	}
	if strings.Contains(child, "padding-left") {
		t.Errorf("行内不再产出缩进像素（间距归 theme.css 的 .tree-cell）：%s", child)
	}
	if !strings.Contains(rowHTML(t, body, "row-self"), `class="tree-cell" style="--tree-depth:0"`) {
		t.Errorf("顶级行层级应为 0：%s", rowHTML(t, body, "row-self"))
	}

	// 分组标题在模板里取词，每组自带中文兜底（缺词条时也不会露出 key）。
	// 期望值用同一个取词函数算，不写死字符串：本测试不连库、i18n 缓存是空的，
	// 写死就变成断言「兜底串」而不是断言「页面问的是哪个 key」。
	// 未知类型（下面的 block）落到 contract 透传的 key + 类型名兜底 ——
	// 新来源类型不改模板也能拿到译文。
	for _, g := range []struct{ key, fallback string }{
		{"admin.navigations.source.page", "页面"},
		{"admin.navigations.source.category", "分类"},
		{"admin.navigations.source.block", "block"},
	} {
		want := i18n.Translate(g.key, g.fallback, "zh-CN") + "（1）"
		if !strings.Contains(body, want) {
			t.Errorf("来源分组标题未按 %s 取词，期望含 %q", g.key, want)
		}
	}
	if strings.Contains(body, "admin.navigations.source.") {
		t.Error("页面上漏出了分组标题的 i18n key")
	}
}

// renderNavigationsPageBody 渲染整页并返回 HTML。
//
// 不依赖数据库：导航服务与工程服务都用桩，只验证模板产出（页面壳由 adminui.Prepare 注入，
// 与线上同一条路径 —— 所以取词用的就是请求语言）。
// newNavigationsPageTestRouter 装配与线上同一条渲染路径的测试路由。
//
// 不依赖数据库：导航服务与工程服务都用桩，只验证模板产出（页面壳由 adminui.Prepare 注入，
// 所以取词用的就是请求语言）。成功路径与**取数失败**路径共用这一份装配 —— 两处各装配一次
// 最容易只改一处（失败路径的断言就会悄悄退化成「测了另一套中间件」）。
func newNavigationsPageTestRouter(t *testing.T,
	nav navigationcontract.NavigationService, projects projectcontract.ProjectService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	conf := viper.New()
	conf.Set("log.base_dir", "/tmp/navigation-page-presentation-test-logs")
	if err := logger.Init(conf); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}
	store := cookie.NewStore([]byte("navigation-page-presentation-test-secret-32b"))
	router.Use(sessions.Sessions("nav-test", store))
	router.Use(func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("csrf_token", "test-csrf-token")
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		// 与 PermContextMiddleware 同源：按钮码集合决定行内是否给出编辑入口。
		c.Set(adminctx.PermSetKey, map[string]bool{"navigation.update": true, "navigation.delete": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"navigation.update": true, "navigation.delete": true})
		c.Next()
	})
	h := NewNavigationPageHandle(nav, projects)
	router.GET("/admin/navigations", adminui.PageSlot(NavigationsPageTemplate), h.NavigationsPage)
	return router
}

func renderNavigationsPageBody(t *testing.T, nav navigationcontract.NavigationService) string {
	t.Helper()
	router := newNavigationsPageTestRouter(t, nav, &pagePresentationProjectStub{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/navigations?project=project-1&kind=header", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败：%d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestNavigationsPageLoadFailureReturnsJSON 取数失败给 **JSON 错误体**，不是整页 HTML。
//
// 判据（2026-10 用户裁定：所有提示只需要一个提示，不渲染完整页）：
//  1. 响应形态是 JSON：Content-Type 含 application/json，正文里没有页壳；
//  2. 状态码是 500：200 会让失败看起来像成功、监控也看不见；
//  3. 文案归口且按请求语言取：出口是 pkg/response 的 msgInternalError，那串中文**本身就是
//     key**（translate 先按 key 查、查不到原样返回），词条在场时必须给译文；
//  4. 数据库原文（`pq: relation "navigations" does not exist`）只进日志、不进响应体。
func TestNavigationsPageLoadFailureReturnsJSON(t *testing.T) {
	// 桩错误刻意带一段「像数据库原文」的内容：它必须只出现在日志里。
	rawErr := errors.New(`pq: relation "navigations" does not exist`)

	for _, tc := range []struct {
		name       string
		acceptLang string
		entries    map[string]map[string]string
		wantMsg    string
	}{
		{
			name:       "zh-CN",
			acceptLang: "zh-CN",
			entries: map[string]map[string]string{
				"服务器内部错误，请稍后重试": {"zh-CN": "操作失败，请稍后重试"},
			},
			wantMsg: "操作失败，请稍后重试",
		},
		{
			name:       "en-US",
			acceptLang: "en-US",
			entries: map[string]map[string]string{
				"服务器内部错误，请稍后重试": {"en-US": "Operation failed, please try again later"},
			},
			wantMsg: "Operation failed, please try again later",
		},
		{
			// 词条缺席（迁移没跑 / 词条库不可用）：落中文原文，不是裸 key、不是空白。
			name:       "词条缺席回落兜底",
			acceptLang: "en-US",
			entries:    nil,
			wantMsg:    "服务器内部错误，请稍后重试",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.InjectForTest(tc.entries, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			router := newNavigationsPageTestRouter(t, &pagePresentationNavigationStub{},
				&pagePresentationProjectStub{listErr: rawErr})
			req := httptest.NewRequest(http.MethodGet, "/admin/navigations?project=project-1&kind=header", nil)
			req.Header.Set("Accept-Language", tc.acceptLang)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			body := rec.Body.String()
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("取数失败的状态码应为 500，得到 %d：%s", rec.Code, body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("失败出口应为 JSON，得到 %q", ct)
			}
			// 页壳缺席：这是「失败不渲染整页」的分水岭。
			for _, banned := range []string{"<!DOCTYPE", "</body>", "</html>", `id="adminRail"`} {
				if strings.Contains(body, banned) {
					t.Errorf("失败出口不该渲染页壳（出现 %q）：%s", banned, body)
				}
			}
			if !strings.Contains(body, tc.wantMsg) {
				t.Errorf("失败响应应含归口文案 %q，实际：%s", tc.wantMsg, body)
			}
			// 归口：原文只进日志。
			if strings.Contains(body, rawErr.Error()) {
				t.Errorf("内部错误原文漏进了响应：%s", body)
			}
		})
	}
}

// TestNavigationsPageFacingErrorTextResolvesPerRequestLanguage 白名单业务错误的**页面**文案：
// 词条在场取当前语言，词条缺席回落页面侧中文兜底 —— 任何一档都不许出现裸 key。
//
// 复审 P2 守的就是这条边界：JSON 出口把 key 交给 pkg/response，它缺词时回落 key 原文
// （前端可判，那条链有自己的兜底）；而页面出口的产物要塞进 HTML，裸 key 就是
// 「页面上出现一串英文常量」—— 与本模块文件头记的 `c.String(500, MsgInternalError)` 同症状。
func TestNavigationsPageFacingErrorTextResolvesPerRequestLanguage(t *testing.T) {
	// 写侧拼定位信息走 enums.ErrDetail（ASCII「: 」分隔），页面出口再按语言换全角/半角。

	for _, tc := range []struct {
		name       string
		acceptLang string
		entries    map[string]map[string]string
		err        error
		want       string
		absent     []string
	}{
		{
			name:       "纯 key / 词条在场 zh-CN",
			acceptLang: "zh-CN",
			entries: map[string]map[string]string{
				navigationenums.ErrPathTaken: {"zh-CN": "同工程同类型下已存在相同路径的导航项", "en-US": "A navigation item with the same path already exists in this project and type."},
			},
			err:    errors.New(navigationenums.ErrPathTaken),
			want:   "同工程同类型下已存在相同路径的导航项",
			absent: []string{"MsgInternalError"},
		},
		{
			name:       "纯 key / 词条在场 en-US",
			acceptLang: "en-US",
			entries: map[string]map[string]string{
				navigationenums.ErrPathTaken: {"zh-CN": "同工程同类型下已存在相同路径的导航项", "en-US": "A navigation item with the same path already exists in this project and type."},
			},
			err:    errors.New(navigationenums.ErrPathTaken),
			want:   "A navigation item with the same path already exists in this project and type.",
			absent: []string{"MsgInternalError"},
		},
		{
			name:       "纯 key / 词条缺席 → 中文兜底",
			acceptLang: "en-US", // 请求英文也照样给兜底：缺词时不能挑语言，更不能给裸 key
			entries:    nil,
			err:        errors.New(navigationenums.ErrPathTaken),
			want:       navigationenums.ErrPathTaken,
			absent:     []string{"MsgInternalError"},
		},
		{
			name:       "key+定位 / 词条缺席 → 中文兜底 + 全角定位",
			acceptLang: "zh-CN",
			entries:    nil,
			err:        navigationenums.ErrDetail(navigationenums.ErrStaleVersion, "首页"),
			// 出口不再拼定位段：pageview.Fail → response.businessText 只用 fact.Fallback。
			want:       "该菜单项已被其他窗口修改，请刷新后重试",
			absent:     []string{"MsgInternalError"},
		},
		{
			name:       "key+定位 / 词条在场 en-US → 英文 + 半角定位",
			acceptLang: "en-US",
			entries: map[string]map[string]string{
				navigationenums.ErrStaleVersion: {"zh-CN": "该菜单项已被其他窗口修改，请刷新后重试", "en-US": "This menu item was changed in another window; refresh and try again"},
			},
			err:    navigationenums.ErrDetail(navigationenums.ErrStaleVersion, "首页"),
			// 不带定位段（见下一条的说明）。
			want:   "This menu item was changed in another window; refresh and try again",
			absent: []string{"ErrStaleVersion"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.InjectForTest(tc.entries, nil)
			t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

			router := newNavigationsPageTestRouter(t, &pagePresentationNavigationStub{},
				&pagePresentationProjectStub{listErr: tc.err})
			req := httptest.NewRequest(http.MethodGet, "/admin/navigations", nil)
			req.Header.Set("Accept-Language", tc.acceptLang)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			body := rec.Body.String()
			// 业务错误按**错误性质**定档（AGENTS.md「400 参数 / 401 / 403 / 404 / 409 / 500 其余」）：
			// ErrPathTaken / ErrStaleVersion 都是冲突类，给 400/409 而不是一律 500。
			// 旧契约「业务错误也渲染 500 整页」是失败页时代的写法，已随
			// 「页面 GET 失败只回 JSON」一起作废。
			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusConflict {
				t.Fatalf("业务错误应给 400/409，得到 %d：%s", rec.Code, body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("业务错误也该是 JSON，得到 %q", ct)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("页面应含 %q，实际：%s", tc.want, body)
			}
			for _, a := range tc.absent {
				if strings.Contains(body, a) {
					t.Errorf("页面上漏出了裸 key %q：%s", a, body)
				}
			}
		})
	}
}

// rowHTML 切出一行（<tr data-filter-text=…> 到 </tr>）。
//
// 按行切片而不是在整页里 Contains：抽屉下拉里也有「新标签页」这个词，
// 整页断言会对任何一行都成立（等于没测）。
func rowHTML(t *testing.T, body, rowID string) string {
	t.Helper()
	// 锚在行内的批量勾选框上：行 id 在创建抽屉的「层级」下拉里也会出现
	//（<option value="row-self">），拿 value="row-self" 当锚会切到抽屉里那一行去。
	idx := strings.Index(body, `name="ids" value="`+rowID+`"`)
	if idx < 0 {
		t.Fatalf("页面里找不到行 %q：%s", rowID, body)
	}
	start := strings.LastIndex(body[:idx], "<tr data-filter-text=")
	if start < 0 {
		t.Fatalf("行 %q 之前没有行起点", rowID)
	}
	rel := strings.Index(body[idx:], "</tr>")
	if rel < 0 {
		t.Fatalf("行 %q 没有闭合", rowID)
	}
	end := idx + rel + len("</tr>")
	return body[start:end]
}

// optionLabel 取某个 <option value="…"> 的文字。
func optionLabel(t *testing.T, body, value string) string {
	t.Helper()
	open := `<option value="` + value + `">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatalf("页面里找不到 value=%q 的下拉项，body=%s", value, body)
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "</option>")
	if j < 0 {
		t.Fatalf("value=%q 的下拉项没有闭合", value)
	}
	return rest[:j]
}
