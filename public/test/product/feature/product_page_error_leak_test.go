package feature

// product_page_error_leak_test.go — 商品后台页不直出内部错误（第三波 CQ-009 形态 ②）。
//
// 传输通道已改为 shell.RenderJump 渲染整页提示（对应 ThinkPHP 的 success() / error()）：
// 写动作的结论走响应体，**不再**经 302 + ?err= 回带列表页。本文件制造一个**真实的
// 基础设施错误** —— 把 products 表改名，查询立刻报 relation "products" does not exist
// (SQLSTATE 42P01) —— 先反证 service 层的原始错误确实带表名与 SQLSTATE，
// 再断言提示页里没有它们、只有归口文案。
//
// 同时断言**业务错误文案仍然原样可见**：归口助手不是「一律吞成通用文案」，
// 否则运营再也看不到「哪一项不合法」。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
)

// productInternalLeakTokens 内部细节指纹：出现任一即视为泄漏。
//
// 取的是「PG 原文与库结构里一定出现、而业务文案里一定不出现」的串：
// 42P01 的原文形如 relation "products" does not exist (SQLSTATE 42P01)。
var productInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

// assertNoProductInternalLeak 断言文本不含任何内部细节指纹。
func assertNoProductInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range productInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

// productNoticeBody 断言响应是**失败态整页提示**（HTTP 200 + data-jump-state="err"）并返回正文。
//
// 取代原先的 productRedirectErr（读 302 Location 上的 ?err=）：结论不再进 URL。
func productNoticeBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("应渲染提示页（200），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("应渲染失败态提示页，body=%s", body)
	}
	return body
}

// assertJumpOK 断言响应是**成功态整页提示**（写动作的结论走 shell.RenderJump，不再 302）。
func assertJumpOK(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("应为成功态提示页（200），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
		t.Fatalf("应为成功态提示页（data-jump-state=ok），body=%s", rec.Body.String())
	}
}

// assertJumpErr 断言响应是**失败态整页提示**并返回正文（取代读 302 Location 上的 ?err=）。
func assertJumpErr(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return productNoticeBody(t, rec)
}

// jumpBackHref 从提示页里取回跳链接的 href（提示页一定有：失败与成功都渲染它）。
//
// Jet 会把属性值里的 & 转义成 &amp;，这里先还原再交给 url.Parse。
func jumpBackHref(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := strings.ReplaceAll(rec.Body.String(), "&amp;", "&")
	re := regexp.MustCompile(`href="(/[^"]*\?[^"]*)"`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("提示页里找不到带 query 的回跳链接：body=%s", rec.Body.String())
	}
	return m[1]
}

// jumpBackQuery 从提示页的回跳链接里取查询参数（取代原先读 302 Location）。
func jumpBackQuery(t *testing.T, rec *httptest.ResponseRecorder, key string) string {
	t.Helper()
	u, err := url.Parse(jumpBackHref(t, rec))
	if err != nil {
		t.Fatalf("回跳链接无法解析：%v", err)
	}
	return u.Query().Get(key)
}

// postProductForm 发一个原生表单 POST（后台页写操作的唯一形态）。
func postProductForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// newProductErrorLeakEnv 装配只挂三个写端点的测试引擎（真实 service + 真实 PG）。
func newProductErrorLeakEnv(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := producthttp.NewProductPageHandle(f.svc, f.projects)
	engine.POST("/admin/products/delete", handle.ProductsDelete)
	engine.POST("/admin/products/tags/create", handle.ProductTagsCreate)
	engine.POST("/admin/products/bulk-delete", handle.ProductsBulkDelete)
	return engine, f
}

// TestProductPageHidesInternalError 真实基础设施错误（products 表不存在）只进日志：
// 提示页必须是归口文案，不含表名 / SQLSTATE / 约束名。
func TestProductPageHidesInternalError(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}
	const pid = "11111111-1111-1111-1111-111111111111"

	// 把依赖「改名」制造真实故障（而不是伪造错误字符串）。
	if err := f.db.Exec("ALTER TABLE products RENAME TO products_hidden").Error; err != nil {
		t.Fatalf("改名 products 表失败：%v", err)
	}
	// 反证：service 层的原始错误确实带表名与 SQLSTATE —— 否则这个用例什么也证明不了。
	rawErr := f.svc.Delete(context.Background(), &productdto.DeleteReq{ID: pid})
	if rawErr == nil {
		t.Fatalf("products 表不存在时删除应失败")
	}
	if !strings.Contains(rawErr.Error(), "products") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误不含表名 / SQLSTATE：%v", rawErr)
	}

	body := productNoticeBody(t, postProductForm(engine, "/admin/products/delete",
		url.Values{"projectId": {f.projectID}, "id": {pid}}))
	assertNoProductInternalLeak(t, "提示页", body)
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("内部错误应给归口文案，body=%s", body)
	}
}

// TestProductPageKeepsBusinessErrorText 业务文案原样可见（白名单命中 → 中文提示）。
func TestProductPageKeepsBusinessErrorText(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}
	// 标签名为空 → service 返回 productenums.ErrTagNameRequired（在白名单里）。
	body := productNoticeBody(t, postProductForm(engine, "/admin/products/tags/create",
		url.Values{"projectId": {f.projectID}, "name": {""}}))
	assertNoProductInternalLeak(t, "提示页", body)
	if !strings.Contains(body, "标签名称必填") {
		t.Fatalf("业务文案必须原样可见，body=%s", body)
	}
}

// TestProductPageKeepsBulkLimitText 受控提示（shell.BulkIDs 的上限拒绝）保持可见：
// 它不是 enums key，但整句由本仓库拼出、带着可行动的数字，收口后不能变成通用文案。
func TestProductPageKeepsBulkLimitText(t *testing.T) {
	engine, f := newProductErrorLeakEnv(t)
	if engine == nil {
		return
	}
	form := url.Values{"projectId": {f.projectID}}
	// 用高于上限一条即触发整批拒绝（不依赖 @10 这类经验数字）。
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
		form.Add("ids", fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	body := productNoticeBody(t, postProductForm(engine, "/admin/products/bulk-delete", form))
	assertNoProductInternalLeak(t, "提示页", body)
	if !strings.Contains(body, "一次最多操作") {
		t.Fatalf("受控提示应保持可见，body=%s", body)
	}
}
