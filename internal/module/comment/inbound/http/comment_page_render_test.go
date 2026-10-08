package commenthttp

// comment_page_render_test.go — 后台审核页的渲染冒烟（BIZ-5）。
//
// 钉住四件事：
//
//   - 模板的 Jet 语法与 layout 数据契约成立（模板写错只会在运营点开时 500，
//     而 500 的表现是通用错误页 —— 不是「少一块内容」那么容易看出来）；
//   - 服务端给的值真的渲染到页面上（不是「接口对了、页面空白」）；
//   - **空态表头常驻**（表头不放进 {{if}}、空态整行进 tbody、colspan 精确等于列数）——
//     门禁 scripts/check-empty-state-table-head.sh 是启发式的，这里给出真实渲染证据；
//   - 降级分支（工程列表读不出来 / 一个工程都没有）也渲染完整页面（缺 key 会 renderError
//     → 整个响应被丢弃）。
//
// 用真实 Jet 渲染器与真实模板文件（不是含内联模板的假 engine）。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	commentdto "go_wp/internal/module/comment/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
	"go_wp/pkg/utils"
)

// stubCommentSvc 后台页需要的 service 替身（只实现页面用到的部分）。
type stubCommentSvc struct {
	res   *commentdto.AdminListResp
	err   error
	types []commentdto.EntityType
}

func (s *stubCommentSvc) ListApproved(context.Context, *commentdto.ListReq) (*commentdto.ListResp, error) {
	return nil, nil
}

func (s *stubCommentSvc) Submit(context.Context, *commentdto.SubmitReq) (*commentdto.SubmitResp, error) {
	return nil, nil
}

func (s *stubCommentSvc) AdminList(context.Context, *commentdto.AdminListReq) (*commentdto.AdminListResp, error) {
	return s.res, s.err
}

func (s *stubCommentSvc) Review(context.Context, *commentdto.ReviewReq) (*commentdto.ReviewResp, error) {
	return nil, nil
}

func (s *stubCommentSvc) EntityTypeLabels(tr func(key, fallback string) string) []commentdto.EntityType {
	out := make([]commentdto.EntityType, 0, len(s.types))
	for _, t := range s.types {
		out = append(out, commentdto.EntityType{Type: t.Type, Label: t.Label})
	}
	return out
}

func (s *stubCommentSvc) IsRegisteredEntityType(string) bool { return true }

func (s *stubCommentSvc) FacingText(string, error) string { return "" }

// stubProjects 工程列表替身（页面只需要「列出全部工程」这一条）。
type stubProjects struct {
	list []projectdto.ProjectResp
	err  error
}

func (s stubProjects) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.list, s.err
}

// renderCommentsPage 用真实渲染器渲染审核页，返回响应体（非 200 直接失败）。
//
// withReviewPerm 控制当前账号是否有 comment:review 权限：它决定列数（勾选列与批量按钮
// 都随权限出现）—— 空态行的 colspan 必须与**实际列数**一致，所以两种形态都要断言。
func renderCommentsPage(t *testing.T, svc *stubCommentSvc, projects stubProjects, query string, withReviewPerm bool) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根相对包目录：internal/module/comment/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	h := NewCommentPageHandle(svc, projects)
	engine.GET("/admin/comments", func(c *gin.Context) {
		perm := map[string]bool{"comment:list": true}
		if withReviewPerm {
			perm["comment:review"] = true
		}
		// 权限集合由 shell.PermContextMiddleware 注入；这一步单独设，
		// 是为了让渲染测试不依赖 admin 权限契约与完整装配链路。
		c.Set(shell.PermSetKey, perm)
		c.Set(shell.ButtonsKey, buttonsOf(perm))
		h.CommentsPage(c)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/comments"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d，响应：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// 整页渲染的判据：响应里必须有完整的收尾标签。
	// 半截 HTML 是本仓渲染器「先渲到 buffer、失败就丢弃」之前的历史故障形态，
	// 这条断言让那种情况不可能悄悄回来。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("响应不是完整页面（缺 </html>），长度 %d", len(body))
	}
	return body
}

// thCount 表头列数（`<thead` 也以 "<th" 开头，先把它扣掉再数）。
func thCount(body string) int {
	return strings.Count(body, "<th") - strings.Count(body, "<thead")
}

// pageFixture 一页待审评论（照 handler 的 templateMap 键集给齐）。
func pageFixture() *stubCommentSvc {
	return &stubCommentSvc{
		types: []commentdto.EntityType{
			{Type: "article", Label: "文章"},
			{Type: "product", Label: "商品"},
		},
		res: &commentdto.AdminListResp{
			Total: 1, Page: 1, PageSize: 20,
			Items: []commentdto.AdminItem{{
				ID: 12, Body: "这条评论在等审核", EntityType: "article", EntityID: "post-1",
				UserID: 1024, Status: "pending", StatusLabel: "待审核",
				CreateTime: utils.NewJSONTime(time.Now()),
			}},
		},
	}
}

// TestCommentsPageRendersRows 页面把内容、实体、评论人、状态与时间都渲染出来。
func TestCommentsPageRendersRows(t *testing.T) {
	body := renderCommentsPage(t, pageFixture(), stubProjects{list: []projectdto.ProjectResp{{ID: "p-1", Name: "演示站"}}}, "", true)

	for _, want := range []string{
		"评论审核",
		"这条评论在等审核",
		"文章", // 实体展示名（来自拥有该实体的模块的词条）
		"post-1",
		"#1024", // 评论人（控制面显示账号 id）
		"待审核",
		// 表头七列齐全（勾选 / 内容 / 评论对象 / 评论人 / 状态 / 提交时间 / 操作）。
		"内容", "评论对象", "评论人", "提交时间", "操作",
		// 批量条：批量表单的目标端点必须出现在页面上。
		"/admin/comments/review",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
}

// TestCommentsPageKeepsTableHeadWhenEmpty 空态时表头必须常驻，且 colspan 等于列数。
func TestCommentsPageKeepsTableHeadWhenEmpty(t *testing.T) {
	svc := pageFixture()
	svc.res = &commentdto.AdminListResp{Total: 0, Page: 1, PageSize: 20, Items: []commentdto.AdminItem{}}
	body := renderCommentsPage(t, svc, stubProjects{list: []projectdto.ProjectResp{{ID: "p-1", Name: "演示站"}}}, "", true)

	if !strings.Contains(body, "<thead>") {
		t.Fatalf("空态时表头必须常驻（用户要能看出有哪些列）：%s", body)
	}
	if !strings.Contains(body, `colspan="7"`) {
		t.Fatalf("空态行的 colspan 应精确等于列数（7）：%s", body)
	}
	if !strings.Contains(body, "没有符合条件的评论") {
		t.Fatalf("空态应给出说明文案：%s", body)
	}
	// 表头列数（含勾选列）必须是 7：少一列时浏览器会把数据行整体左移且不报错。
	// 计数扣掉 `<thead` —— 它同样以 "<th" 开头，直接 Count("<th") 会多算一个。
	if got := thCount(body); got != 7 {
		t.Fatalf("表头列数应为 7，实际 %d", got)
	}
}

// TestCommentsPageKeepsTableHeadWithoutReviewPerm 没有审核权限时列数少一列（勾选列），
// colspan 必须跟着变 —— 固定写死 7 会让那一行多出一格，浏览器静默左移整表。
func TestCommentsPageKeepsTableHeadWithoutReviewPerm(t *testing.T) {
	svc := pageFixture()
	svc.res = &commentdto.AdminListResp{Total: 0, Page: 1, PageSize: 20, Items: []commentdto.AdminItem{}}
	body := renderCommentsPage(t, svc, stubProjects{list: []projectdto.ProjectResp{{ID: "p-1", Name: "演示站"}}}, "", false)

	if !strings.Contains(body, "<thead>") {
		t.Fatalf("无审核权限时表头同样常驻：%s", body)
	}
	if !strings.Contains(body, `colspan="6"`) {
		t.Fatalf("无审核权限时 colspan 应为 6（与表头列数一致）：%s", body)
	}
	if got := thCount(body); got != 6 {
		t.Fatalf("无审核权限时表头列数应为 6，实际 %d", got)
	}
}

// TestCommentsPageRendersWithoutProjects 一个工程都没有时是明确的空态（不是空白列表）。
func TestCommentsPageRendersWithoutProjects(t *testing.T) {
	body := renderCommentsPage(t, pageFixture(), stubProjects{}, "", true)
	if !strings.Contains(body, "还没有站点工程") {
		t.Fatalf("无工程时应给明确空态：%s", body)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatal("空态也要渲染完整页面")
	}
}

// TestCommentsPageDegradesOnLoadFailure 读库失败时给归口文案而不是内部错误原文。
func TestCommentsPageDegradesOnLoadFailure(t *testing.T) {
	body := renderCommentsPage(t, &stubCommentSvc{err: errors.New(`pq: relation "comments" does not exist`)}, stubProjects{
		list: []projectdto.ProjectResp{{ID: "p-1", Name: "演示站"}},
	}, "", true)
	if strings.Contains(body, `relation "comments"`) {
		t.Fatalf("页面不该出现数据库原文：%s", body)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatal("降级分支也要渲染完整页面（缺 key 会整页 500）")
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
