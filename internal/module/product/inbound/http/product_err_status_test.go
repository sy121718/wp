package producthttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

type errStatusSvc struct {
	productcontract.ProductService
	err error
}

func (s errStatusSvc) Get(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) {
	return nil, s.err
}

func (s errStatusSvc) Update(context.Context, *productdto.UpdateReq) (*productdto.ProductResp, error) {
	return nil, s.err
}

func (s errStatusSvc) Create(context.Context, *productdto.CreateReq) (*productdto.ProductResp, error) {
	return nil, s.err
}

func (s errStatusSvc) SaveVariantList(context.Context, *productdto.SaveVariantListReq) (*productdto.SaveVariantListResp, error) {
	return nil, s.err
}

func productErrStatusRequest(t *testing.T, err error, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	engine.Use(view.FailureMiddleware(adminui.RenderFailure))
	engine.Use(view.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	h := NewProductHandle(errStatusSvc{err: err})
	engine.GET("/product/get", h.Get)
	engine.POST("/product/update", view.HTMLFragment, h.Update)
	engine.POST("/product/create", view.HTMLFragment, h.Create)
	engine.POST("/product/variant/save", h.SaveVariantList)

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if path == "/product/create" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// productErrWords 给这一组用例注入生产词条的真实 key 与中英固定文案：
// 声明里不再抄中文，出口只认词库，断言必须是"词库里那句"而不是"非空"。
func productErrWords(t *testing.T) {
	t.Helper()
	i18n.InjectForTest(map[string]map[string]string{
		productenums.ErrNameRequired: {"zh-CN": "商品名称必填"},
		productenums.ErrNotFound:     {"zh-CN": "商品或变体不存在"},
		productenums.ErrSlugTaken:    {"zh-CN": "该 URL 段在本工程已被占用"},
		productenums.ErrVariantStockDuplicated: {
			"zh-CN": "库存清单里同一仓库同一 SKU 出现了多行，请合并后再保存",
		},
		response.InternalKey: {"zh-CN": response.InternalFallback},
	}, nil)
}

func TestProductErrStatusComesFromDeclaration(t *testing.T) {
	productErrWords(t)

	// 受控业务明细（半角 ": " 后的纯文本）是调用方给的事实，出口按原样保留。
	conflictWithDetail := fmt.Errorf("%w: %s", productenums.Err(productenums.ErrSlugTaken), "summer-sale")

	for _, tt := range []struct {
		name      string
		err       error
		method    string
		path      string
		body      string
		wantCode  int
		wantInMsg string

		wantNotInMsg string
	}{
		{
			name: "参数不合法默认 400", err: productenums.Err(productenums.ErrNameRequired),
			method: http.MethodPost, path: "/product/create", body: "name=x",
			wantCode: http.StatusBadRequest, wantInMsg: "商品名称必填",
		},
		{
			name: "声明过 404 的 key 给 404", err: productenums.Err(productenums.ErrNotFound),
			method: http.MethodGet, path: "/product/get?projectId=p1&id=p-1",
			wantCode: http.StatusNotFound, wantInMsg: "商品或变体不存在",
		},
		{
			name: "声明过 409 的 key 给 409", err: productenums.Err(productenums.ErrSlugTaken),
			method: http.MethodPost, path: "/product/update", body: `{"id":"p-1"}`,
			wantCode: http.StatusConflict, wantInMsg: "该 URL 段在本工程已被占用",
		},
		{
			name: "带受控明细的冲突仍是 409 且明细上屏", err: conflictWithDetail,
			method: http.MethodPost, path: "/product/update", body: `{"id":"p-1"}`,
			wantCode: http.StatusConflict, wantInMsg: "该 URL 段在本工程已被占用：summer-sale",
		},
		{
			name: "声明过 400 的库存重复给 400", err: productenums.Err(productenums.ErrVariantStockDuplicated),
			method: http.MethodPost, path: "/product/variant/save", body: `{"productId":"p-1"}`,
			wantCode: http.StatusBadRequest, wantInMsg: "同一仓库同一 SKU",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := productErrStatusRequest(t, tt.err, tt.method, tt.path, tt.body)
			if tt.path == "/product/create" || tt.path == "/product/update" {
				assertProductHTMLWrite(t, rec, tt.wantCode, "data-ui-error")
			}
			if rec.Code != tt.wantCode {
				t.Fatalf("状态码 = %d，want %d（%s）", rec.Code, tt.wantCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantInMsg) {
				t.Fatalf("对外文案应含 %q，实际 %s", tt.wantInMsg, rec.Body.String())
			}
			if tt.wantNotInMsg != "" && strings.Contains(rec.Body.String(), tt.wantNotInMsg) {
				t.Fatalf("明细 %q 不该出现在对外文案，实际 %s", tt.wantNotInMsg, rec.Body.String())
			}
		})
	}
}

func TestProductInternalErrorIsFiveHundredAndOpaque(t *testing.T) {
	productErrWords(t)
	rec := productErrStatusRequest(t,
		errors.New(`pq: relation "products" does not exist`),
		http.MethodGet, "/product/get?projectId=p1&id=p-1", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("内部错误状态码 = %d，want 500（%s）", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leak := range []string{"pq:", "SQLSTATE", `"products"`, "does not exist"} {
		if strings.Contains(body, leak) {
			t.Fatalf("内部原文泄漏到对外文案（%q）：%s", leak, body)
		}
	}
	if !strings.Contains(body, response.InternalFallback) {
		t.Fatalf("应给出归口文案，实际 %s", body)
	}
}
