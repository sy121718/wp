package feature

import (
	"context"
	"fmt"
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/middleware/adminctx"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"

	"go_wp/public/test/support"
)

func newBrandPagingEnv(t *testing.T) (*gin.Engine, *gorm.DB, *projectservice.Service) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil, nil, nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := productservice.NewService(productmodel.NewModel(db), projects)

	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"product:brand_create": true, "product:brand_update": true, "product:brand_delete": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"product.brand_create": true, "product.brand_update": true, "product.brand_delete": true,
		})

	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	h := producthttp.NewProductPageHandle(svc, svc)
	engine.GET("/admin/product-brands", pageview.UseRenderer(adminui.RenderPage), h.ProductBrandsPage)
	return engine, db, projects
}

const brandSeedSplit = 25

func seedBrands(t *testing.T, db *gorm.DB, projectID string, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	values := make([]string, 0, n)
	args := make([]any, 0, n*5)
	for i := 0; i < n; i++ {
		id := uuid.NewString()
		ids = append(ids, id)
		prefix := "乙"
		if i < brandSeedSplit {
			prefix = "甲"
		}
		values = append(values, "(?, ?, ?, ?, ?)")
		args = append(args, id, projectID, fmt.Sprintf("%s品牌 %04d", prefix, i), fmt.Sprintf("brand-%04d", i), i)
	}
	sql := "INSERT INTO product_brands (id, project_id, name, slug, sort) VALUES " + strings.Join(values, ", ")
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("批量造品牌失败：%v", err)
	}
	return ids
}

func brandPageBody(t *testing.T, engine *gin.Engine, query string) string {
	t.Helper()
	rec := httptestGet(engine, "/admin/product-brands?"+query)
	if rec.Code != http.StatusOK {
		t.Fatalf("品牌页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func brandRowCount(body string) int {
	return strings.Count(body, `name="ids"`)
}

func TestBrandPagePagingFollowsSourceCount(t *testing.T) {
	engine, db, projects := newBrandPagingEnv(t)
	if engine == nil {
		return
	}
	p, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "品牌分页工程"})
	if err != nil {
		t.Fatalf("建测试工程失败: %v", err)
	}
	projectID := p.ID
	seedBrands(t, db, projectID, 45)

	const perPage = 20

	first := brandPageBody(t, engine, "project="+projectID)
	if got := brandRowCount(first); got != perPage {
		t.Fatalf("第 1 页应渲染 %d 行，实际 %d 行（分页没生效 = 又在全量渲染）", perPage, got)
	}
	if !strings.Contains(first, "共 45 条，第 1-20 条") {
		t.Fatalf("分页信息应取 CountBrands 的真源总数（共 45 条）：%s", first[:min(len(first), 600)])
	}
	if !strings.Contains(first, "甲品牌 0000") || strings.Contains(first, "乙品牌 0044") {
		t.Fatalf("第 1 页应是排序最前的 20 个品牌（甲品牌 0000-0019）")
	}

	third := brandPageBody(t, engine, fmt.Sprintf("project=%s&page=3", projectID))
	if got := brandRowCount(third); got != 5 {
		t.Fatalf("第 3 页应渲染 5 行（45 = 20 + 20 + 5），实际 %d 行", got)
	}
	if !strings.Contains(third, "共 45 条，第 41-45 条") || !strings.Contains(third, "乙品牌 0044") {
		t.Fatalf("第 3 页应是「第 41-45 条」且包含乙品牌 0044")
	}

	overflow := brandPageBody(t, engine, fmt.Sprintf("project=%s&page=9", projectID))
	if got := brandRowCount(overflow); got != 5 {
		t.Fatalf("越界页码应收敛到最后一页（5 行），实际 %d 行", got)
	}
	if !strings.Contains(overflow, "共 45 条，第 41-45 条") {
		t.Fatalf("越界页码收敛后分页信息应与最后一页一致")
	}

	filtered := brandPageBody(t, engine, "project="+projectID+"&keyword="+url.QueryEscape("甲品牌"))
	if got := brandRowCount(filtered); got != perPage {
		t.Fatalf("关键词「甲品牌」命中 25 条，第 1 页应渲染 %d 行，实际 %d 行", perPage, got)
	}
	if !strings.Contains(filtered, "共 25 条，第 1-20 条") {
		t.Fatalf("筛选后的总数应随过滤条件收紧（共 25 条），而不是沿用未筛选的 45：%s", filtered[:min(len(filtered), 600)])
	}
	if strings.Contains(filtered, "乙品牌") {
		t.Fatalf("筛选结果里不应出现不匹配的品牌")
	}

	single := brandPageBody(t, engine, "project="+projectID+"&keyword="+url.QueryEscape("乙品牌"))
	if got := brandRowCount(single); got != 20 {
		t.Fatalf("关键词「乙品牌」恰好 20 条，应渲染 20 行，实际 %d 行", got)
	}
	if strings.Contains(single, `class="pagination"`) {
		t.Fatalf("总量恰好一页时不应渲染分页条")
	}
}
