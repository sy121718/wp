package producthttp

// product_err_status_test.go — 状态码在**错误声明处**给，出口只读它。
//
// 这一条以前是控制器按接口写死的：`Get` 固定传 404、写动作固定传 400。于是同一份错误
// 换个接口就换了含义 —— 读库失败的内部错误被报成「商品不存在」（404），而「URL 段被占用」
// 被报成 400，前端按状态码做的分支与监控分类都跟着错，且没有任何地方报错。
//
// 现在四档的判据各有一条（业务默认 400 / 声明 404 / 声明 409 / 非业务 500），
// 断言的是**状态码 + 对外文案**，不是实现细节：
//   · 400 与 404/409 的差别必须来自 enums 的声明（改文案不能改状态码）；
//   · 内部错误一律 500，且 SQL 原文不能出现在响应体里。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// errStatusSvc 只实现本组用例会调到的方法；其余由嵌入的接口兜底。
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

// productErrStatusRequest 发一次请求并返回 recorder（方法 / 路径 / body 由用例给）。
func productErrStatusRequest(t *testing.T, err error, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	h := NewProductHandle(errStatusSvc{err: err})
	engine.GET("/product/get", h.Get)
	engine.POST("/product/update", h.Update)
	engine.POST("/product/create", h.Create)

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestProductErrStatusComesFromDeclaration 四档状态码各一条。
func TestProductErrStatusComesFromDeclaration(t *testing.T) {
	conflictWithDetail := fmt.Errorf("%w: %s", productenums.Err(productenums.ErrSlugTaken), "summer-sale")

	for _, tt := range []struct {
		name      string
		err       error
		method    string
		path      string
		body      string
		wantCode  int
		wantInMsg string
		// wantNotInMsg 明细不进对外文案：Tail 来自「%w: 明细」，而明细常常是底层 err 原文
		//（enums.ErrDetail(key, err.Error()) 这种写法就是），出口无条件透出等于开一个泄漏口。
		wantNotInMsg string
	}{
		{
			name: "参数不合法默认 400", err: productenums.Err(productenums.ErrNameRequired),
			method: http.MethodPost, path: "/product/create", body: `{"name":"x"}`,
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
			name: "带明细的冲突仍是 409", err: conflictWithDetail,
			method: http.MethodPost, path: "/product/update", body: `{"id":"p-1"}`,
			wantCode: http.StatusConflict, wantInMsg: "该 URL 段在本工程已被占用", wantNotInMsg: "summer-sale",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := productErrStatusRequest(t, tt.err, tt.method, tt.path, tt.body)
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

// TestProductInternalErrorIsFiveHundredAndOpaque 非业务错误：500 + 归口文案，原文只进日志。
func TestProductInternalErrorIsFiveHundredAndOpaque(t *testing.T) {
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
	if !strings.Contains(body, "服务器内部错误") {
		t.Fatalf("应给出归口文案，实际 %s", body)
	}
}
