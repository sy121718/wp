package navigationhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/casbin"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

type editPermissionNavigation struct {
	navigationcontract.NavigationService
	getCalls int
}

func (s *editPermissionNavigation) Get(_ context.Context, _ *navigationdto.GetReq) (*navigationdto.NavigationResp, error) {
	s.getCalls++
	return nil, nil
}

func TestNavigationEditUsesPostUpdatePermission(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if err := casbin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := casbin.InitCasbin(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = casbin.Close() })

	svc := &editPermissionNavigation{}
	r := gin.New()
	pages := r.Group("/admin", func(c *gin.Context) { c.Set("user_id", int64(987654321)) })
	navigationhttp.SetupNavigationPages(pages, svc, projectcontract.ProjectService(nil), pagecontract.PageService(nil), blockcontract.BlockService(nil))
	request := func() int {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/navigations/edit?id=row-1&project=project-1&kind=header", nil))
		return rec.Code
	}
	if status := request(); status != http.StatusForbidden || svc.getCalls != 0 {
		t.Fatalf("没有更新权限应在读行前拒绝: status=%d get=%d", status, svc.getCalls)
	}
	if _, err := casbin.GetEnforcer().AddPolicy("987654321", "/api/navigation/update", http.MethodPost, "navigation:update"); err != nil {
		t.Fatal(err)
	}
	// A permitted request reaches Get and receives 404 for this missing fixture.
	if status := request(); status != http.StatusNotFound || svc.getCalls != 1 {
		t.Fatalf("真实 POST 更新权限应允许 GET 入口: status=%d get=%d", status, svc.getCalls)
	}
}
