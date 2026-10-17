package producthttp

// product_jump_test.go — 共享出口原语：筛选上下文与批量 id 归一。
//
// 提示页（shell.RenderJump）与回跳地址（shell.BackPath）已从模块层删除：写动作走
// ui/json_write.js 的 JSON 基座（结论进 flash 行），页面 GET 失败走页内失败槽或 302。
// 留下的是两件仍被生产代码使用的东西：
//
//  1. 各页的**筛选上下文键表**（渲染时拼进表单 action 的 query，POST/GET 回来时按同一份
//     白名单读回）—— 漏一个键的症状是「翻页 / 提交后筛选静默丢了」，不报错也不记日志；
//  2. 批量 id 归一（pkg/bulkids）的超限出口 —— 拒绝时要把上限与本次条数都告诉操作者。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
)

// newProductJumpContext 造一个带真实 Jet 渲染器的请求上下文（页面渲染断言用）。
func newProductJumpContext(t *testing.T, hxHeader, rawQuery, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	target := "/admin/products"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	c.Request = httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if hxHeader != "" {
		c.Request.Header.Set("HX-Request", hxHeader)
	}
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c, rec
}

// TestProductQueryFromRequestDropsEmptyAndUnknown 筛选上下文只透传列出的键、空值丢弃。
func TestProductQueryFromRequestDropsEmptyAndUnknown(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "project=p1&keyword=&status=&source=x", "")
	if got := productQueryFromRequest(c, productListBackKeys...); got != "project=p1" {
		t.Errorf("productQueryFromRequest = %q，want project=p1", got)
	}
	// 站内路径拼接：空上下文原样返回（不留悬空的 ?）。
	if got := withListQuery("/admin/products", ""); got != "/admin/products" {
		t.Errorf("withListQuery 空上下文 = %q", got)
	}
	if got := withListQuery("/admin/products", "project=p1"); got != "/admin/products?project=p1" {
		t.Errorf("withListQuery = %q", got)
	}
	// 已有 query 时用 & 连接。
	if got := withListQuery("/x?a=1", "project=p1"); got != "/x?a=1&project=p1" {
		t.Errorf("withListQuery 追加 = %q", got)
	}
}

// TestProductFilterKeysKeepContext 各页键表保留自己需要的上下文，且不透传未列出的键。
//
// 键表是页面表单 action 与读回侧共用的唯一一份：删掉一个键不会编译报错，
// 只会让那一页的筛选在翻页 / 提交后静默丢失。
func TestProductFilterKeysKeepContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet,
		"/x?project=p1&keyword=abc&product=prod1&lang=en-US&source=BundleSourceProduct", nil)

	if got := productQueryFromRequest(c, productListBackKeys...); !strings.Contains(got, "project=p1") ||
		!strings.Contains(got, "keyword=abc") || strings.Contains(got, "source=") {
		t.Errorf("列表键表 = %q（应含 project/keyword、不含未列出的 source）", got)
	}
	if got := productQueryFromRequest(c, productPricingBackKeys...); !strings.Contains(got, "project=p1") {
		t.Errorf("定价键表 = %q（应保留 project）", got)
	}
	if got := productQueryFromRequest(c, productTranslationsBackKeys...); !strings.Contains(got, "lang=en-US") {
		t.Errorf("翻译键表 = %q（应保留 lang）", got)
	}
	if got := productQueryFromRequest(c, productBundleBackKeys...); !strings.Contains(got, "product=prod1") {
		t.Errorf("捆绑键表 = %q（应保留 product）", got)
	}
}
