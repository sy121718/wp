package feature

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
)

// TestProductRatingHTMLZeroAndScope 验证零分、body 隔离、跨工程拒绝及重复删除。
func TestProductRatingHTMLZeroAndScope(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		t.Fatal("评分专项必须使用真实 PostgreSQL")
	}
	ctx := t.Context()
	p, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "零分商品", Slug: "rating-zero"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "其他评分工程"})
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"projectId": {f.projectID}, "productId": {p.ID}, "score": {"0"}}
	body := assertProductHTMLResponse(t, postProductHTMLForm(engine, "/api/product/rating/add?score=5&projectId="+other.ID, values), http.StatusOK)
	if !strings.Contains(body, `<span data-product-rating="0">0</span>`) || !strings.Contains(body, "1 条") {
		t.Fatalf("合法零分不能当作未评分：%s", body)
	}
	rating, err := f.svc.ListRatings(ctx, &productdto.ListRatingsReq{ProjectID: f.projectID, ProductID: p.ID})
	if err != nil || len(rating.Items) != 1 || !rating.HasRating || rating.Rating != 0 || rating.ProductID != p.ID || rating.ProjectID != f.projectID {
		t.Fatalf("零分及归属必须真实落库：%v %+v", err, rating)
	}
	values.Set("projectId", other.ID)
	assertProductHTMLResponse(t, postProductHTMLForm(engine, "/api/product/rating/add", values), http.StatusNotFound)
	assertProductHTMLResponse(t, postProductHTMLForm(engine, "/api/product/rating/delete", url.Values{"projectId": {other.ID}, "id": {rating.Items[0].ID}}), http.StatusNotFound)
	// 仅 query 提供评分 ID 不能让写入 body 的必填校验通过。
	assertProductHTMLResponse(t, postProductHTMLForm(engine, "/api/product/rating/delete?id="+rating.Items[0].ID, url.Values{"projectId": {f.projectID}}), http.StatusBadRequest)
	still, err := f.svc.ListRatings(ctx, &productdto.ListRatingsReq{ProjectID: f.projectID, ProductID: p.ID})
	if err != nil || len(still.Items) != 1 || still.Items[0].ID != rating.Items[0].ID {
		t.Fatalf("跨工程和非法输入不得修改评分：%v %+v", err, still)
	}
	deleted := url.Values{"projectId": {f.projectID}, "id": {rating.Items[0].ID}}
	assertProductHTMLResponse(t, postProductHTMLForm(engine, "/api/product/rating/delete", deleted), http.StatusOK)
	assertProductHTMLResponse(t, postProductHTMLForm(engine, "/api/product/rating/delete", deleted), http.StatusNotFound)
	// 普通评分端点不接受旧 JSON 提交链。
	assertProductHTMLResponse(t, postJSON(t, engine, "/api/product/rating/add", map[string]any{"projectId": f.projectID, "productId": p.ID, "score": 5}), http.StatusBadRequest)
}
