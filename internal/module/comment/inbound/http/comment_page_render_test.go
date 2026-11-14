package commenthttp

import (
	"context"
	"errors"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/adminctx"
	commentdto "go_wp/internal/module/comment/dto"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/utils"
)

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
		out = append(out, commentdto.EntityType{Type: t.Type, Key: t.Key, Label: t.Label})
	}
	return out
}

func (s *stubCommentSvc) IsRegisteredEntityType(string) bool { return true }

func (s *stubCommentSvc) FacingText(string, error) string { return "" }

type stubProjects struct {
	list []projectdto.ProjectResp
	err  error
}

func (s stubProjects) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.list, s.err
}

func renderCommentsPage(t *testing.T, svc *stubCommentSvc, projects stubProjects, query string, withReviewPerm bool) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	// 生产装配在基础层挂请求取词作用域：模板的 t / tf 依赖它。
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	h := NewCommentPageHandle(svc, projects)
	engine.GET("/admin/comments", pageview.Slot(CommentsPageTemplate, pageview.RenderPage), func(c *gin.Context) {
		perm := map[string]bool{"comment:list": true}
		if withReviewPerm {
			perm["comment:review"] = true
		}

		c.Set(adminctx.PermSetKey, perm)
		c.Set(adminctx.ButtonsKey, buttonsOf(perm))
		h.CommentsPage(c)
	})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/comments"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态 %d，响应：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, "</html>") {
		t.Fatalf("响应不是完整页面（缺 </html>），长度 %d", len(body))
	}
	return body
}

func thCount(body string) int {
	return strings.Count(body, "<th") - strings.Count(body, "<thead")
}

func pageFixture() *stubCommentSvc {
	return &stubCommentSvc{
		types: []commentdto.EntityType{
			{Type: "article", Key: "admin.article.list.heading", Label: "文章"},
			{Type: "product", Key: "trTypeProd", Label: "商品"},
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

func TestCommentsPageRendersRows(t *testing.T) {
	body := renderCommentsPage(t, pageFixture(), stubProjects{list: []projectdto.ProjectResp{{ID: "p-1", Name: "演示站"}}}, "", true)
	if strings.Contains(body, "/admin/comments/review") {
		t.Fatal("页面仍含旧表单写入口")
	}

	for _, want := range []string{
		"评论审核",
		"这条评论在等审核",
		"文章",
		"post-1",
		"#1024",
		"待审核",

		"内容", "评论对象", "评论人", "提交时间", "操作",

		`hx-post="/api/comment/review"`,
		`data-list-bulk`,

		`data-write-result`,
		`name="status" value="approved" data-requires-js hidden`,
		`name="status" value="rejected" data-requires-js hidden`,
		`data-write-status role="status"`, `name="projectId" value="p-1"`,
		`name="ids" value="12"`,
		`meta name="csrf-token"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
}

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

	if got := thCount(body); got != 7 {
		t.Fatalf("表头列数应为 7，实际 %d", got)
	}
}

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

func TestCommentsPageRendersWithoutProjects(t *testing.T) {
	body := renderCommentsPage(t, pageFixture(), stubProjects{}, "", true)
	if !strings.Contains(body, "还没有站点工程") {
		t.Fatalf("无工程时应给明确空态：%s", body)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatal("空态也要渲染完整页面")
	}
}

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

func buttonsOf(perms map[string]bool) map[string]bool {
	out := make(map[string]bool, len(perms))
	for code := range perms {
		out[strings.ReplaceAll(code, ":", ".")] = true
	}
	return out
}
