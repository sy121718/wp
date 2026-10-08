// product_brand_page_paging_test.go — 品牌页真源分页的行为契约（审计 02-M 的 D12）。
//
// 改动前的现状：ListBrands 不传分页参数、handler 全量取回再 listPageSlice 切片 ——
// 每翻一页都把整张品牌表读进内存。本批把 Page/Size 加进 ListBrandReq、model 支持
// LIMIT/OFFSET、service 透传，handler 改成「先计数 → 收敛页码 → 再取当页」。
//
// 本文件钉住四件事（都与标签页的 product_tag_page_query_budget_test.go 同形，
// 但走的是**另一条代码路径**：brand 的 model 分页参数、CountBrands 的过滤口径）：
//  1. 每页只取一页的数据（行数按 productSubListPageSize 变化，而不是「全量渲染」）；
//  2. 分页信息取契约 Count 的真源总数；
//  3. 越界页码收敛到最后一页（不是空表）——「先计数再取页」的直接判据；
//  4. 关键词筛选下计数与列表用**同一份过滤条件**（总数随筛选收紧，且单页时无分页条）。
package feature

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/internal/shell"

	"go_wp/public/test/support"
)

// newBrandPagingEnv 装配只挂品牌管理页的测试引擎（真实 service + 真实 PG + 真实模板）。
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
	// 页面组由 shell.PermContextMiddleware 注入权限码；这条链路不挂鉴权中间件，
	// 注入一份权限，保证页面按真实形态渲染（品牌页的批量条与行内按钮按它显隐）。
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{
			"product:brand_create": true, "product:brand_update": true, "product:brand_delete": true,
		})
		c.Set(shell.ButtonsKey, map[string]bool{
			"product.brand_create": true, "product.brand_update": true, "product.brand_delete": true,
		})

	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	h := producthttp.NewProductPageHandle(svc, projects)
	engine.GET("/admin/product-brands", h.ProductBrandsPage)
	return engine, db, projects
}

// brandSeedSplit 前 25 个品牌叫「甲品牌 NN」、其余 20 个叫「乙品牌 NN」。
//
// 名字里嵌一个可筛前缀，是为了让「关键词筛选下计数与列表同一份过滤条件」这条判据
// 有一次**跨页**的命中集合（25 条 = 两页）：只命中 10 条那种单页场景下，
// 分页条本就不渲染（BuildPagination 在 total 不超过一页时返回 nil），
// 「共 N 条」的文案根本不会出现 —— 断言会看上去像失败，其实什么都没测到。
const brandSeedSplit = 25

// seedBrands 批量造 n 个品牌：sort = i（排序键是 sort ASC, create_time ASC, id ASC），
// 于是第 1 页永远是 sort 最小的那一批，页码与内容的对应关系确定。
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

// brandPageBody 请求品牌页并返回 HTML（非 200 直接失败）。
func brandPageBody(t *testing.T, engine *gin.Engine, query string) string {
	t.Helper()
	rec := httptestGet(engine, "/admin/product-brands?"+query)
	if rec.Code != http.StatusOK {
		t.Fatalf("品牌页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// brandRowCount 页面渲染出的品牌行数（每行一个批量勾选框 name="ids"）。
func brandRowCount(body string) int {
	return strings.Count(body, `name="ids"`)
}

// TestBrandPagePagingFollowsSourceCount 多页场景：按页取数据、真源总数、越界收敛。
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

	const perPage = 20 // = handler 的 productSubListPageSize（未导出，此处对齐并留注）

	// —— 第 1 页：只取一页 ——
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

	// —— 第 3 页：余数页 ——
	third := brandPageBody(t, engine, fmt.Sprintf("project=%s&page=3", projectID))
	if got := brandRowCount(third); got != 5 {
		t.Fatalf("第 3 页应渲染 5 行（45 = 20 + 20 + 5），实际 %d 行", got)
	}
	if !strings.Contains(third, "共 45 条，第 41-45 条") || !strings.Contains(third, "乙品牌 0044") {
		t.Fatalf("第 3 页应是「第 41-45 条」且包含乙品牌 0044")
	}

	// —— 越界页码：收敛到最后一页 ——
	// 顺序反过来（先取页再 count）时，service 会为第 9 页返回空页，而分页条按收敛后的页码
	// 渲染 —— 页面就成了「表格为空、分页条显示第 3 页」。
	overflow := brandPageBody(t, engine, fmt.Sprintf("project=%s&page=9", projectID))
	if got := brandRowCount(overflow); got != 5 {
		t.Fatalf("越界页码应收敛到最后一页（5 行），实际 %d 行", got)
	}
	if !strings.Contains(overflow, "共 45 条，第 41-45 条") {
		t.Fatalf("越界页码收敛后分页信息应与最后一页一致")
	}

	// —— 关键词：计数与列表同一份过滤条件（口径分叉时总数与行数会对不上）——
	// 关键词必须走 url 编码：中文 + 空格直接拼进请求行会让 httptest 判定「畸形 HTTP 版本」。
	// 「甲品牌」命中 25 条（跨两页），分页信息与行数都由**筛选后**的真源给出。
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

	// —— 单页边界：total 恰好等于每页条数时 pages=1，分页条不渲染 ——
	single := brandPageBody(t, engine, "project="+projectID+"&keyword="+url.QueryEscape("乙品牌"))
	if got := brandRowCount(single); got != 20 {
		t.Fatalf("关键词「乙品牌」恰好 20 条，应渲染 20 行，实际 %d 行", got)
	}
	if strings.Contains(single, `class="pagination"`) {
		t.Fatalf("总量恰好一页时不应渲染分页条")
	}
}
