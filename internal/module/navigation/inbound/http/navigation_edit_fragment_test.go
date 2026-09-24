package navigationhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/auth"
	"go_wp/pkg/logger"

	"github.com/spf13/viper"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

type editNavigationStub struct {
	navigationcontract.NavigationService
	item       *navigationdto.NavigationResp
	getErr     error
	updateErr  error
	lastUpdate *navigationdto.UpdateReq
	getCalls   int
}

func (s *editNavigationStub) Get(_ context.Context, req *navigationdto.GetReq) (*navigationdto.NavigationResp, error) {
	s.getCalls++
	if s.item != nil && req.ID != s.item.ID {
		return nil, nil
	}
	return s.item, s.getErr
}

func (s *editNavigationStub) Update(_ context.Context, req *navigationdto.UpdateReq) (*navigationdto.NavigationResp, error) {
	s.lastUpdate = req
	return s.item, s.updateErr
}

type editBlocksStub struct {
	BlockPanelPort
	list    []blockcontract.BlockResp
	created *blockcontract.BlockResp
}

func (s *editBlocksStub) List(_ context.Context, _ *blockcontract.ListReq) ([]blockcontract.BlockResp, error) {
	return s.list, nil
}

func (s *editBlocksStub) Create(_ context.Context, _ *blockcontract.CreateReq) (*blockcontract.BlockResp, error) {
	return s.created, nil
}

func editRouter(t *testing.T, h *navigationPageHandle) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	conf := viper.New()
	conf.Set("log.base_dir", "/tmp/navigation-edit-fragment-test-logs")
	if err := logger.Init(conf); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(nil); err != nil {
		t.Fatal(err)
	}
	store := cookie.NewStore([]byte("navigation-edit-fragment-test-secret-32-bytes"))
	r.Use(sessions.Sessions("nav-test", store))
	r.Use(func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("csrf_token", "test-csrf-token")
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		c.Next()
	})
	r.GET("/admin/navigations/edit", h.NavigationEditFragment)
	r.POST("/admin/navigations/update", h.NavigationUpdate)
	r.POST("/admin/navigations/panel", h.PanelSet)
	r.POST("/admin/navigations/panel/create", h.PanelCreate)
	return r
}

func TestNavigationEditFragment(t *testing.T) {
	item := &navigationdto.NavigationResp{ID: "row-1", ProjectID: "project-1", Kind: "header", Title: "旧标题", Path: "/old", Target: "self", UpdatedAt: "2026-09-01T12:00:00.123456Z"}
	svc := &editNavigationStub{item: item}
	h := NewNavigationPageHandle(svc, nil)
	h.SetBlockPanelPort(&editBlocksStub{list: []blockcontract.BlockResp{{ID: "block-1", Name: "面板一"}}})
	r := editRouter(t, h)
	for _, tc := range []struct {
		name, query string
		status      int
		fragments   bool
	}{
		{"valid", "id=row-1&project=project-1&kind=header", 200, true},
		{"wrong project", "id=row-1&project=project-2&kind=header", 404, false},
		{"wrong kind", "id=row-1&project=project-1&kind=footer", 404, false},
		{"missing id", "project=project-1&kind=header", 400, false},
		{"invalid kind", "id=row-1&project=project-1&kind=unknown", 400, false},
		{"missing project", "id=row-1&kind=header", 400, false},
		{"not found", "id=missing&project=project-1&kind=header", 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/navigations/edit?"+tc.query, nil))
			if rec.Code != tc.status {
				t.Fatalf("status=%d, body=%s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !tc.fragments {
				if strings.Contains(body, "旧标题") || strings.Contains(body, "data-drawer-fragment") {
					t.Fatalf("错误响应泄露行: %s", body)
				}
				return
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("headers=%v", rec.Header())
			}
			for _, want := range []string{`data-drawer-fragment`, `name="csrf_token" value="`, `name="expectedUpdatedAt" value="2026-09-01T12:00:00.123456Z"`, `name="projectId" value="project-1"`, `旧标题`, `面板一`, `hx-post="/admin/navigations/update"`} {
				if !strings.Contains(body, want) {
					t.Errorf("片段缺少 %q: %s", want, body)
				}
			}
			if strings.Count(body, "data-drawer-fragment") != 1 || strings.Contains(body, "<html") {
				t.Fatalf("片段根结构异常: %s", body)
			}
		})
	}
	svc.getErr = errors.New("database private detail")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/navigations/edit?id=row-1&project=project-1&kind=header", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "database private detail") {
		t.Fatalf("内部错误泄露: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNavigationEditPostRejectsOtherContext(t *testing.T) {
	for _, tc := range []struct{ name, path, project, kind string }{
		{"update project", "/admin/navigations/update", "project-2", "header"},
		{"update kind", "/admin/navigations/update", "project-1", "footer"},
		{"panel project", "/admin/navigations/panel", "project-2", "header"},
		{"panel kind", "/admin/navigations/panel", "project-1", "footer"},
		{"panel create project", "/admin/navigations/panel/create", "project-2", "header"},
		{"panel create kind", "/admin/navigations/panel/create", "project-1", "footer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &editNavigationStub{item: &navigationdto.NavigationResp{ID: "row-1", ProjectID: "project-1", Kind: "header"}}
			h := NewNavigationPageHandle(svc, nil)
			r := editRouter(t, h)
			values := url.Values{"id": {"row-1"}, "projectId": {tc.project}, "kind": {tc.kind}, "title": {"意外写入"}, "expectedUpdatedAt": {"old-token"}}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound || svc.lastUpdate != nil || strings.Contains(rec.Body.String(), "意外写入") {
				t.Fatalf("跨上下文请求应在写入前拒绝: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestNavigationUpdateHTMXRetainsSubmittedValues(t *testing.T) {
	item := &navigationdto.NavigationResp{ID: "row-1", ProjectID: "project-1", Kind: "header", Title: "库中标题", Path: "/saved", Target: "self", UpdatedAt: "new-token"}
	svc := &editNavigationStub{item: item, updateErr: errors.New("database private detail")}
	h := NewNavigationPageHandle(svc, nil)
	r := editRouter(t, h)
	values := url.Values{"id": {"row-1"}, "projectId": {"project-1"}, "kind": {"header"}, "title": {"我写的标题"}, "path": {"/submitted"}, "target": {"blank"}, "expectedUpdatedAt": {"old-token"}}
	request := func(hx bool) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/navigations/update", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if hx {
			req.Header.Set("HX-Request", "true")
		}
		r.ServeHTTP(rec, req)
		return rec
	}
	rec := request(true)
	if rec.Code != 200 || rec.Header().Get("HX-Redirect") != "" {
		t.Fatalf("HTMX 失败响应: %d %v", rec.Code, rec.Header())
	}
	for _, want := range []string{`data-drawer-fragment`, `name="csrf_token" value="`, `value="我写的标题"`, `value="/submitted"`, `value="old-token"`, `value="blank"`, `role="alert"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("失败回填缺少 %q: %s", want, rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), "database private detail") || strings.Contains(rec.Body.String(), "库中标题") {
		t.Fatalf("失败响应错误或覆盖提交值: %s", rec.Body.String())
	}
	if svc.lastUpdate == nil || svc.lastUpdate.ExpectedUpdatedAt == nil || *svc.lastUpdate.ExpectedUpdatedAt != "old-token" {
		t.Fatalf("乐观锁未传递: %+v", svc.lastUpdate)
	}
	native := request(false)
	if native.Code != http.StatusSeeOther || native.Header().Get("HX-Redirect") != "" {
		t.Fatalf("原生提交行为变更: %d %v", native.Code, native.Header())
	}
	svc.updateErr = nil
	success := request(true)
	if success.Code != 200 || !strings.Contains(success.Header().Get("HX-Redirect"), "project=project-1") || success.Header().Get("Location") != "" {
		t.Fatalf("HTMX 成功应整页跳转: %d %v", success.Code, success.Header())
	}
}

func TestNavigationEditPostRequiresVersionToken(t *testing.T) {
	for _, path := range []string{"/admin/navigations/update", "/admin/navigations/panel"} {
		t.Run(path, func(t *testing.T) {
			svc := &editNavigationStub{item: &navigationdto.NavigationResp{ID: "row-1", ProjectID: "project-1", Kind: "header"}}
			r := editRouter(t, NewNavigationPageHandle(svc, nil))
			values := url.Values{"id": {"row-1"}, "projectId": {"project-1"}, "kind": {"header"}}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || svc.lastUpdate != nil {
				t.Fatalf("缺失版本令牌应拒绝更新: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestNavigationPanelCreateUsesNativeRedirect(t *testing.T) {
	svc := &editNavigationStub{item: &navigationdto.NavigationResp{ID: "row-1", ProjectID: "project-1", Kind: "header"}}
	h := NewNavigationPageHandle(svc, nil)
	h.SetBlockPanelPort(&editBlocksStub{created: &blockcontract.BlockResp{ID: "block-1"}})
	r := editRouter(t, h)
	values := url.Values{"id": {"row-1"}, "projectId": {"project-1"}, "kind": {"header"}, "title": {"我的菜单"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/navigations/panel/create", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/workbench?block=block-1") || rec.Header().Get("HX-Redirect") != "" {
		t.Fatalf("新建块应走原生 POST 跳转: %d %v", rec.Code, rec.Header())
	}
}

func TestNavigationPanelHTMXRetainsExplicitClear(t *testing.T) {
	oldPanel := "old-block"
	svc := &editNavigationStub{item: &navigationdto.NavigationResp{
		ID: "row-1", ProjectID: "project-1", Kind: "header", Title: "数据库标题", Path: "/saved",
		Target: "self", PanelBlockID: &oldPanel, PanelWidth: "full", UpdatedAt: "new-token",
	}, updateErr: errors.New("private database detail")}
	h := NewNavigationPageHandle(svc, nil)
	h.SetBlockPanelPort(&editBlocksStub{list: []blockcontract.BlockResp{{ID: oldPanel, Name: "旧块"}}})
	r := editRouter(t, h)
	values := url.Values{"id": {"row-1"}, "projectId": {"project-1"}, "kind": {"header"},
		"menu": {"row-1"}, "title": {"用户标题"}, "path": {""}, "target": {"blank"},
		"panelBlockId": {""}, "panelWidth": {"auto"}, "expectedUpdatedAt": {"old-token"}}
	request := func(hx bool) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/navigations/panel", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if hx {
			req.Header.Set("HX-Request", "true")
		}
		r.ServeHTTP(rec, req)
		return rec
	}
	rec := request(true)
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "" {
		t.Fatalf("失败分档: %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-drawer-fragment`, `value="用户标题"`, `name="path" class="form-input" value=""`, `name="expectedUpdatedAt" value="old-token"`, `value="auto" selected`, `role="alert"`} {
		if !strings.Contains(body, want) {
			t.Errorf("回填缺少 %q: %s", want, body)
		}
	}
	if strings.Contains(body, `value="old-block" selected`) || strings.Contains(body, "数据库标题") || strings.Contains(body, "private database detail") {
		t.Fatalf("清空面板或输入值被库中旧值覆盖: %s", body)
	}
	if svc.lastUpdate == nil || svc.lastUpdate.PanelBlockID == nil || *svc.lastUpdate.PanelBlockID != "" {
		t.Fatalf("面板清空未传给服务: %+v", svc.lastUpdate)
	}
	if native := request(false); native.Code != http.StatusSeeOther {
		t.Fatalf("原生失败: %d", native.Code)
	}
	svc.updateErr = nil
	if success := request(true); success.Code != http.StatusOK || success.Header().Get("HX-Redirect") == "" {
		t.Fatalf("面板成功: %d %v", success.Code, success.Header())
	}
}
