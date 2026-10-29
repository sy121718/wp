package feature

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	workbenchhttp "go_wp/internal/module/workbench/inbound/http"
	"go_wp/internal/templates"
	"go_wp/pkg/casbin"
	"go_wp/public/test/support"
	"gorm.io/gorm"
)

// 原路由、真实PG与真实编译一起验证：预览只是读，不借用Page草稿或保存块。
func TestBlockDraftPreviewReadonlyScopedAndVersioned(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(t.Context(), &projectdto.CreateReq{Name: "块草稿预览"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := projects.Create(t.Context(), &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatal(err)
	}
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	block, err := blocks.Create(t.Context(), &blockdto.CreateReq{ProjectID: project.ID, Name: "未保存预览", Document: []byte(workbenchStructureDoc)})
	if err != nil {
		t.Fatal(err)
	}
	block, err = blocks.Detail(t.Context(), &blockdto.DetailReq{ProjectID: project.ID, ID: block.ID})
	if err != nil {
		t.Fatal(err)
	}
	pages := pageservice.NewService(pagemodel.NewPageModel(db), nil, nil, projects, blocks, nil, nil, nil, nil)
	_ = casbin.Close()
	if err := casbin.InitCasbin(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = casbin.Close() })
	if _, err := casbin.GetEnforcer().AddPolicy("810", "/api/block/update", "POST", "block:update"); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	group := engine.Group("")
	group.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-User"), 10, 64)
		c.Set("user_id", id)
		c.Next()
	})
	workbenchhttp.SetupWorkbenchRoutes(group, pages, projects, blocks, nil, nil, nil, nil, nil, nil, nil)
	writes := 0
	for name, register := range map[string]func(string, func(*gorm.DB)) error{
		"create": db.Callback().Create().Before("gorm:create").Register,
		"update": db.Callback().Update().Before("gorm:update").Register,
		"delete": db.Callback().Delete().Before("gorm:delete").Register,
	} {
		if err := register("test:preview-no-write-"+name, func(*gorm.DB) { writes++ }); err != nil {
			t.Fatal(err)
		}
	}
	post := func(user, projectID, version, document string) *httptest.ResponseRecorder {
		form := url.Values{"id": {block.ID}, "projectId": {projectID}, "expectedVersion": {version}, "draftDocument": {document}}
		req := httptest.NewRequest(http.MethodPost, "/workbench/block/preview?previewState=empty", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Test-User", user)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	version := strconv.FormatInt(block.Version, 10)
	for _, tc := range []struct {
		name, user, project, version string
		status                       int
	}{
		{"未登录", "", project.ID, version, 401}, {"无编辑权限", "811", project.ID, version, 403},
		{"跨工程", "810", other.ID, version, 404}, {"过期版本", "810", project.ID, "999", 409},
		{"缺少工程", "810", "", version, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(tc.user, tc.project, tc.version, workbenchStructureDraftDoc)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
	rec := post("810", project.ID, version, workbenchStructureDraftDoc)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "STRUCTURE-DRAFT-MARK") || strings.Contains(rec.Body.String(), "STRUCTURE-NO-ENTITY-MARK") {
		t.Fatalf("未保存草稿未参与编译：%d %s", rec.Code, firstN(rec.Body.String(), 500))
	}
	if !strings.Contains(rec.Body.String(), `name="site-preview-mode" content="readonly"`) || !strings.Contains(rec.Body.String(), `name="site-preview-state" content="empty"`) {
		t.Fatal("块预览丢失统一只读/模拟上下文")
	}
	emptyImage := `{"settings":{"layout":{"mode":"full"}},"root":[{"type":"core.image","id":"draft-empty-image","props":{}}]}`
	rec = post("810", project.ID, version, emptyImage)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "draft-empty-image") || !strings.Contains(rec.Body.String(), "data-sky-incomplete") {
		t.Fatalf("编辑中空图应局部待完善：%d %s", rec.Code, firstN(rec.Body.String(), 700))
	}
	after, err := blocks.Detail(t.Context(), &blockdto.DetailReq{ProjectID: project.ID, ID: block.ID})
	if err != nil {
		t.Fatal(err)
	}
	if writes != 0 || after.Version != block.Version || !bytes.Equal(after.Document, block.Document) || after.UpdatedAt != block.UpdatedAt {
		t.Fatalf("预览发生持久写：writes=%d beforeVersion=%d afterVersion=%d", writes, block.Version, after.Version)
	}
}
