package masterdatahttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/templates"
)

const mdProjectListErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

type mdFailingProjects struct {
	projectcontract.ProjectService
}

func (mdFailingProjects) List(context.Context) ([]projectcontract.ProjectResp, error) {
	return nil, errors.New(mdProjectListErrText)
}

type mdCountingChanges struct {
	masterdatacontract.MasterDataService
	calls int
}

func (m *mdCountingChanges) ListChanges(context.Context, *masterdatadto.ListChangeReq) ([]*masterdatadto.ChangeResp, error) {
	m.calls++
	return nil, errors.New("装载失败时不应取数")
}

func (m *mdCountingChanges) CountChanges(context.Context, *masterdatadto.ListChangeReq) (int64, error) {
	m.calls++
	return 0, errors.New("装载失败时不应取数")
}

func (m *mdCountingChanges) ListEntities(context.Context, *masterdatadto.ListEntityReq) ([]*masterdatadto.EntityHistoryResp, error) {
	m.calls++
	return nil, errors.New("装载失败时不应取数")
}

func (m *mdCountingChanges) CountEntities(context.Context, *masterdatadto.ListEntityReq) (int64, error) {
	m.calls++
	return 0, errors.New("装载失败时不应取数")
}

func (m *mdCountingChanges) EntityTimeline(context.Context, *masterdatadto.EntityTimelineReq) (*masterdatadto.EntityTimelineResp, error) {
	m.calls++
	return nil, errors.New("装载失败时不应取数")
}

func TestMasterDataChangesPageDegradesWhenProjectListFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	changes := &mdCountingChanges{}
	handle := NewMasterDataChangePageHandle(changes, mdFailingProjects{})
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/masterdata/changes",
		adminui.PageSlot(MasterDataChangesTemplate),
		handle.MasterDataChangesPage)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/masterdata/changes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染页面（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, "</html>") {
		t.Fatalf("装载失败后页面必须渲染完整（缺 </html>）：%s", body)
	}

	for _, want := range []string{`name="entityType"`, `name="entityId"`, `role="alert"`, "按实体汇总"} {
		if !strings.Contains(body, want) {
			t.Errorf("装载失败后页面骨架必须还在（缺 %q）—— 不能退回一块裸文本", want)
		}
	}

	for _, want := range []string{"系统内部错误，请稍后重试", "页面数据未取到：", "列表没读出来"} {
		if !strings.Contains(body, want) {
			t.Errorf("装载失败页面缺少受控提示 %q", want)
		}
	}
	for _, forbidden := range []string{
		"uiInternalErr",
		"SQLSTATE", "projects\"", "42P01",
		"没有符合条件的变更记录", "没有任何变更记录",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("装载失败页面不应出现 %q", forbidden)
		}
	}

	if changes.calls != 0 {
		t.Fatalf("装载失败时不应调用变更记录服务，实际调用 %d 次", changes.calls)
	}
}
