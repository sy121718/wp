package feature

import (
	mw "go_wp/pkg/middleware"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	json "go_wp/pkg/jsonx"
)

func TestPublicProductOptionsOnlyExposesEnabledFacts(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	id := f.createProduct(t, "设计杯", "options-cup", "", 29.95, 39.95)
	draft := f.createProduct(t, "未发布杯", "options-draft", "", 10, 20)
	published := productenums.StatusPublished
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: id, Status: &published}); err != nil {
		t.Fatal(err)
	}
	product, err := f.products.Get(ctx, &productdto.GetReq{ID: id, ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	disabled, cost := false, 4.5
	if _, err = f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: product.Variants[1].ID, Enabled: &disabled, CostPrice: &cost}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	producthttp.SetupProductOptionsRoutes(mw.NewRegistry(), engine, f.products)
	read := func(project, productID string, status int) *productdto.PublicProductOptionsResp {
		t.Helper()
		rec := httptest.NewRecorder()
		q := url.Values{"projectId": {project}, "productId": {productID}}
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/site/product/options?"+q.Encode(), nil))
		if rec.Code != status {
			t.Fatalf("状态错误 %d: %s", rec.Code, rec.Body.String())
		}
		for _, key := range []string{"costPrice", "CostPrice", "barcode", "<form", "priceLabel"} {
			if strings.Contains(rec.Body.String(), key) {
				t.Fatalf("泄露内部或展示字段 %q: %s", key, rec.Body.String())
			}
		}
		var body struct {
			Message string                               `json:"message"`
			Data    *productdto.PublicProductOptionsResp `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if status >= 400 && (body.Message == "" || strings.Contains(body.Message, "SQL")) {
			t.Fatalf("错误未归口: %s", rec.Body.String())
		}
		return body.Data
	}
	data := read(f.projectID, id, 200)
	if data.ID != id || data.Name != "设计杯" || len(data.Variants) != 1 || data.Variants[0].ID != product.Variants[0].ID || data.Variants[0].Price != 2995 {
		t.Fatalf("公开规格事实错误: %+v", data)
	}
	read(f.projectID, draft, 404)
	read(uuid.NewString(), id, 404)
	read(f.projectID, "bad", 400)
}
