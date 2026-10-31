package feature

import (
	"context"
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

type scopeTransmitFixture struct {
	*variantListFixture
	otherProject string
}

func newScopeTransmitEngine(t *testing.T) (*gin.Engine, *scopeTransmitFixture) {
	t.Helper()
	base := newVariantListFixture(t)
	if base == nil {
		return nil, nil
	}

	base.products.SetAvailabilityPort(base.inventory)
	other, err := base.projects.Create(context.Background(), &projectdto.CreateReq{Name: "第二站点工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	if other.ID == base.projectID {
		t.Fatalf("第二个工程 id 与第一个相同：%s", other.ID)
	}
	projects, lerr := base.projects.List(context.Background())
	if lerr != nil {
		t.Fatalf("读工程清单失败: %v", lerr)
	}
	if len(projects) != 2 {
		t.Fatalf("本文件的判据要求多工程夹具，实际 %d 个工程", len(projects))
	}
	f := &scopeTransmitFixture{variantListFixture: base, otherProject: other.ID}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	grantProductPerms(engine)
	handle := producthttp.NewProductPageHandle(base.products, base.products)
	engine.GET("/admin/products/detail", pageview.UseRenderer(adminui.RenderPage), handle.ProductDetailPage)
	engine.POST("/admin/products/variant/preview", handle.PreviewVariant)
	return engine, f
}

func (f *scopeTransmitFixture) scopeTransmitProduct(t *testing.T, name, slug string, attrIDs []string) *productdto.ProductResp {
	t.Helper()
	price := 88.0
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
		AttributeIDs: attrIDs, DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return res
}

func (f *scopeTransmitFixture) scopeTransmitAttr(t *testing.T) *productdto.AttributeResp {
	t.Helper()
	attr, err := f.products.CreateAttribute(context.Background(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color",
		Values: []productdto.AttributeValueReq{{Label: "红", Key: "red"}},
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	return attr
}

func scopeTransmitValueID(t *testing.T, attr *productdto.AttributeResp, key string) string {
	t.Helper()
	for _, v := range attr.Values {
		if v.Key == key {
			return v.ID
		}
	}
	t.Fatalf("属性组 %s 里没有值 %s", attr.Key, key)
	return ""
}

func TestProductDetailPageTransmitsProjectScope(t *testing.T) {
	engine, f := newScopeTransmitEngine(t)
	if engine == nil {
		return
	}
	p := f.scopeTransmitProduct(t, "作用域详情商品", "scope-detail-product", nil)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/detail?project="+f.projectID+"&product="+p.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("多工程站点上打开商品详情页应 200，实际 %d（取商品那一跳漏传作用域 → ErrInvalidParam → 400）：%s",
			rec.Code, oneLine(rec.Body.String()))
	}
	if body := rec.Body.String(); !strings.Contains(body, p.Name) {
		t.Fatalf("详情页应渲染出商品名 %q（不是失败页）：%s", p.Name, oneLine(body))
	}
}

func TestVariantPreviewTransmitsProjectScope(t *testing.T) {
	engine, f := newScopeTransmitEngine(t)
	if engine == nil {
		return
	}
	attr := f.scopeTransmitAttr(t)
	valueID := scopeTransmitValueID(t, attr, "red")
	p := f.scopeTransmitProduct(t, "作用域预览商品", "scope-transmit-preview", []string{attr.ID})

	rec := postForm(engine, "/admin/products/variant/preview", url.Values{
		"projectId":       {f.projectID},
		"productId":       {p.ID},
		"warehouseId":     {f.warehouse.ID},
		"attr:" + attr.ID: {valueID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("预览应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	payload := jsonMap(t, rec.Body.Bytes())
	if payload["code"] != float64(http.StatusOK) {
		t.Fatalf("预览必须使用统一成功外壳：%+v", payload)
	}
	data := asMap(t, payload["data"], "data")
	if data["productId"] != p.ID {
		t.Fatalf("预览商品不一致：%+v", data)
	}
	rows := asList(t, data["rows"], "data.rows")
	if len(rows) != 1 {
		t.Fatalf("勾选 1 个值应返回 1 行，实际 %d：%+v", len(rows), payload)
	}
	row := asMap(t, rows[0], "rows[0]")
	values := asMap(t, row["optionValues"], "optionValues")
	if len(values) != 1 || values["color"] != "red" || row["skuCode"] == "" {
		t.Fatalf("预览应保留规范组合与 SKU：%+v", row)
	}
	if _, exists := row["spec"]; exists {
		t.Fatalf("规格成品句不应回到后端：%+v", row)
	}
	page := productDetailBody(t, engine, f.projectID, p.ID)
	product := productDisplayData(t, page)
	if product.ProjectID != f.projectID || len(product.Attributes) != 1 || product.Attributes[0].ID != attr.ID || product.Attributes[0].Name != "颜色" || product.Attributes[0].Values[0].Label != "红" {
		t.Fatalf("页面必须交付同工程原属性定义，不能退化为空属性：%+v", product.Attributes)
	}
}

func (f *scopeTransmitFixture) generateCombosFor(t *testing.T, productID, attributeID, valueID string) {
	t.Helper()
	ctx := context.Background()
	pv, err := f.products.PreviewVariantCombinations(ctx, &productdto.PreviewVariantReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		Selections: []productdto.VariantSelectionReq{{AttributeID: attributeID, ValueIDs: []string{valueID}}},
	})
	if err != nil {
		t.Fatalf("预览组合失败: %v", err)
	}
	rows := make([]productdto.VariantListResp, 0, len(pv.Rows))
	for _, row := range pv.Rows {
		rows = append(rows, productdto.VariantListResp{OptionValues: row.OptionValues})
	}
	if _, err = f.products.SaveVariantList(ctx, &productdto.SaveVariantListReq{
		ProductID: productID, ProjectID: f.projectID, WarehouseID: f.warehouse.ID,
		OperatorID: "tester", Rows: rows,
	}); err != nil {
		t.Fatalf("保存变体清单失败: %v", err)
	}
}
