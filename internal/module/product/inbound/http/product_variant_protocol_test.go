package producthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/facing"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type variantProtocolService struct{ productcontract.ProductService }

func (*variantProtocolService) SaveVariantList(context.Context, *productdto.SaveVariantListReq) (*productdto.SaveVariantListResp, error) {
	return nil, &productdto.BulkRejectedError{Err: productenums.Err(productenums.ErrBulkRejected), Items: []facing.BlockedItem{{ID: "v1", Name: "SKU1", ReasonKey: productenums.ErrVariantListDuplicated, Fallback: productenums.VariantSkipFallback(productenums.ErrVariantListDuplicated)}}}
}
func TestVariantSaveRejectsWholeListWithJSONItems(t *testing.T) {
	engine := gin.New()
	engine.POST("/api/product/variant/save", NewProductHandle(&variantProtocolService{}).SaveVariantList)
	req := httptest.NewRequest(http.MethodPost, "/api/product/variant/save", strings.NewReader("{\"productId\":\"p1\",\"rows\":[]}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var body struct {
		Code    int
		Message string
		Data    struct {
			Items []struct {
				ID      string
				Name    string
				Message string
			}
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict || body.Code != http.StatusConflict || len(body.Data.Items) != 1 || body.Data.Items[0].ID != "v1" || body.Data.Items[0].Name != "SKU1" || body.Data.Items[0].Message == "" {
		t.Fatalf("整批拒绝须给真实409与定位明细：%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "skipped") || strings.Contains(body.Data.Items[0].Message, "已忽略") {
		t.Fatalf("拒绝不得描述为部分成功：%s", rec.Body.String())
	}
}
