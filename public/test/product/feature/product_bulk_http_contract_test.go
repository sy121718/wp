package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
)

func postProductHTMLForm(engine *gin.Engine, path string, values url.Values) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	engine.ServeHTTP(rec, req)
	return rec
}

func assertProductHTMLResponse(t *testing.T, rec *httptest.ResponseRecorder, status int) string {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != status || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("应为 HTML %d，实际 %d %s：%s", status, rec.Code, rec.Header().Get("Content-Type"), body)
	}
	for _, forbidden := range []string{"SQLSTATE", "<html", "<body", `"code":`, "pNotFound"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("局部界面不应含 %q：%s", forbidden, body)
		}
	}
	if status >= 400 && !strings.Contains(body, "data-ui-error") {
		t.Fatalf("失败未经过共享 Jet 出口：%s", body)
	}
	if rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" || rec.Header().Get("HX-Location") != "" {
		t.Fatal("普通写结果不能由后端控制导航")
	}
	return body
}

func TestProductBulkHTTPContract(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		t.Fatal("本专项必须使用真实 PostgreSQL")
	}

	t.Run("超过上限：400 且文案带两个数字", func(t *testing.T) {
		ids := make([]string, 201)
		for i := range ids {
			ids[i] = uuid.NewString()
		}
		rec := postProductHTMLForm(engine, "/api/product/bulk-delete", url.Values{"projectId": {f.projectID}, "ids": ids})
		body := assertProductHTMLResponse(t, rec, http.StatusBadRequest)
		if !strings.Contains(body, "200") || !strings.Contains(body, "201") {
			t.Fatalf("超限文案必须保留上限与本次条数：%s", body)
		}
	})

	t.Run("没勾选：400 而不是删除零个的成功", func(t *testing.T) {
		rec := postProductHTMLForm(engine, "/api/product/bulk-delete", url.Values{"projectId": {f.projectID}})
		body := assertProductHTMLResponse(t, rec, http.StatusBadRequest)
		if !strings.Contains(body, "请求参数无效") {
			t.Fatalf("空勾选必须给可读原因：%s", body)
		}
	})

	t.Run("夹着不存在的ID：409定位条目且整批零写入", func(t *testing.T) {
		p, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "批量契约商品", Slug: "bulk-http-contract"})
		if err != nil {
			t.Fatal(err)
		}
		const missing = "11111111-1111-4111-8111-111111111111"
		rec := postProductHTMLForm(engine, "/api/product/bulk-delete", url.Values{"projectId": {f.projectID}, "ids": {p.ID, missing}})
		body := assertProductHTMLResponse(t, rec, http.StatusConflict)
		if !strings.Contains(body, "批量操作被拒绝") || !strings.Contains(body, missing) || strings.Count(body, "<li>") != 1 || !strings.Contains(body, "不存在") {
			t.Fatalf("应只列出一个缺失条目及可读原因：%s", body)
		}
		if _, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: p.ID}); err != nil {
			t.Fatalf("整批被拒时合法商品不能被删：%v", err)
		}
	})

	t.Run("合法集合全部删除", func(t *testing.T) {
		ids := []string{}
		for _, slug := range []string{"bulk-html-first", "bulk-html-second"} {
			p, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: slug, Slug: slug})
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, p.ID)
		}
		rec := postProductHTMLForm(engine, "/api/product/bulk-delete", url.Values{"projectId": {f.projectID}, "ids": ids})
		body := assertProductHTMLResponse(t, rec, http.StatusOK)
		if !strings.Contains(body, `role="status"`) {
			t.Fatal("成功必须由 Jet 展示")
		}
		for _, id := range ids {
			if _, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: id}); err == nil {
				t.Fatalf("成功后商品 %s 仍存在", id)
			}
		}
	})
}
