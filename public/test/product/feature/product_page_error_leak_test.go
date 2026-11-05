package feature

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	pageview "go_wp/pkg/view"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	bulkids "go_wp/pkg/bulkids"
)

var productInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

func assertNoProductInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range productInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

func newProductErrorLeakEnv(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := producthttp.NewProductHandle(f.svc)
	engine.POST("/api/product/delete", pageview.HTMLFragment, handle.Delete)
	engine.POST("/api/product/bulk-delete", pageview.HTMLFragment, handle.BulkDelete)
	return engine, f
}

func TestProductPageHidesInternalError(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}
	const pid = "11111111-1111-1111-1111-111111111111"

	if err := f.db.Exec("ALTER TABLE products RENAME TO products_hidden").Error; err != nil {
		t.Fatalf("改名 products 表失败：%v", err)
	}

	rawErr := f.svc.Delete(context.Background(), &productdto.DeleteReq{ID: pid})
	if rawErr == nil {
		t.Fatalf("products 表不存在时删除应失败")
	}
	if !strings.Contains(rawErr.Error(), "products") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误不含表名 / SQLSTATE：%v", rawErr)
	}

	rec := postForm(engine, "/api/product/delete", url.Values{"projectId": {f.projectID}, "id": {pid}})
	body := rec.Body.String()
	if rec.Code == http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("内部错误不该回成功：%d %s", rec.Code, body)
	}
	assertNoProductInternalLeak(t, "HTML 错误区", body)
	if !strings.Contains(body, "data-ui-error") || !strings.Contains(body, "系统内部错误") {
		t.Fatalf("内部错误应给归口文案：%s", body)
	}
}

func TestProductPageKeepsBusinessErrorText(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}

	rec := postForm(engine, "/api/product/delete", url.Values{"projectId": {f.projectID}, "id": {"11111111-1111-1111-1111-111111111111"}})
	body := rec.Body.String()
	if rec.Code == http.StatusOK || !strings.Contains(body, "data-ui-error") {
		t.Fatalf("删不存在的商品应失败并渲染错误区：%d %s", rec.Code, body)
	}
	assertNoProductInternalLeak(t, "HTML 错误区", body)
	if strings.Contains(body, productenums.ErrNotFound) || strings.TrimSpace(body) == "" {
		t.Fatalf("业务错误应由 Jet 取词，不该输出裸 key：%s", body)
	}
}

func TestProductPageKeepsBulkLimitText(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}

	ids := make([]string, 0, bulkids.MaxIDs+1)
	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	values := url.Values{"projectId": {f.projectID}}
	for _, id := range ids {
		values.Add("ids", id)
	}
	rec := postForm(engine, "/api/product/bulk-delete", values)
	body := rec.Body.String()
	if rec.Code == http.StatusOK || !strings.Contains(body, "data-ui-error") {
		t.Fatalf("超限应整批拒绝并渲染错误区：%d %s", rec.Code, body)
	}
	assertNoProductInternalLeak(t, "HTML 错误区", body)
	if !strings.Contains(body, "一次最多操作") {
		t.Fatalf("受控提示应保持可见：%s", body)
	}
	if !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(body, strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("受控提示应带上限与本次条数：%s", body)
	}
}
