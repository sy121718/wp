package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	json "go_wp/pkg/jsonx"
)

type publicAvailability struct {
	seen    []string
	project string
}

func (p *publicAvailability) AvailableQuantities(_ context.Context, project string, ids []string) (map[string]int, error) {
	p.seen, p.project = ids, project
	result := map[string]int{}
	for _, id := range ids {
		result[id] = 0
	}
	return result, nil
}

func TestPublicVariantData(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	productID := f.createProduct(t, "公开商品", "public-data", "", 129, 199)
	draftID := f.createProduct(t, "草稿商品", "draft-data", "", 99, 199)
	published := productenums.StatusPublished
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: productID, Status: &published}); err != nil {
		t.Fatal(err)
	}
	product, err := f.products.Get(ctx, &productdto.GetReq{ID: productID, ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := f.products.Get(ctx, &productdto.GetReq{ID: draftID, ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	id, draftVariant, missing := product.Variants[0].ID, draft.Variants[0].ID, uuid.NewString()
	stock := &publicAvailability{}
	f.products.SetAvailabilityPort(stock)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	producthttp.SetupProductPublicRoutes(engine, f.products)
	read := func(project, ids string, status int) []productdto.PublicVariantResp {
		t.Helper()
		rec := httptest.NewRecorder()
		q := url.Values{"projectId": {project}, "variantIds": {ids}, "prices": {"1"}, "currency": {"evil"}}
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/site/product/variants?"+q.Encode(), nil))
		if rec.Code != status || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("状态/类型错误: %d %s", rec.Code, rec.Body.String())
		}
		for _, forbidden := range []string{"CostPrice", "costPrice", "<span", "以结算", "价格已更新"} {
			if strings.Contains(rec.Body.String(), forbidden) {
				t.Fatalf("数据接口泄露展示或内部字段 %q: %s", forbidden, rec.Body.String())
			}
		}
		var body struct {
			Code    int                            `json:"code"`
			Message string                         `json:"message"`
			Data    []productdto.PublicVariantResp `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if status >= 400 && (body.Message == "" || strings.Contains(body.Message, "SQL")) {
			t.Fatalf("错误未归口: %s", rec.Body.String())
		}
		return body.Data
	}
	got := read(f.projectID, id+","+draftVariant+","+missing+","+id, 200)
	if len(got) != 3 || !got[0].Known || got[0].Price == nil || *got[0].Price != 12900 || got[0].Available == nil || *got[0].Available != 0 {
		t.Fatalf("公开商品事实错误: %+v", got)
	}
	if got[1].Known || got[1].Price != nil || got[1].Available != nil || got[2].Known {
		t.Fatalf("草稿/不存在应未知: %+v", got)
	}
	wantIDs := make([]string, 0, len(product.Variants))
	for _, v := range product.Variants {
		if v.ID == id {
			wantIDs = append(wantIDs, id)
		}
	}
	if !reflect.DeepEqual(stock.seen, wantIDs) || stock.project != f.projectID {
		t.Fatalf("库存查询必须只含公开商品且传工程: %+v %s", stock.seen, stock.project)
	}
	price := 149.0
	if _, err := f.products.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: id, Price: &price}); err != nil {
		t.Fatal(err)
	}
	if next := read(f.projectID, id, 200); len(next) != 1 || *next[0].Price != 14900 {
		t.Fatalf("改价应立即读到，不依赖重新构建: %+v", next)
	}
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "公开接口隔离工程"})
	if err != nil {
		t.Fatal(err)
	}
	if cross := read(other.ID, id, 200); len(cross) != 1 || cross[0].Known || cross[0].Price != nil || cross[0].Available != nil {
		t.Fatalf("跨工程不得返回事实: %+v", cross)
	}
	read(f.projectID, "not-a-uuid", 400)
	read("", id, 400)
	read(f.projectID, strings.TrimSuffix(strings.Repeat(id+",", 101), ","), 400)
}
