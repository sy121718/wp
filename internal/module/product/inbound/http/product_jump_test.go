package producthttp

// product_jump_test.go — product 后台页写动作出口的判据守卫（就近单测，不碰数据库）。
//
// 传输通道已改为 shell.RenderJump 渲染整页提示（见 product_jump.go）：写动作的结论走
// 响应体，**不再**经 302/303 + `?err=` / `?done=` / `?applied=` / `?saved=` 回带。
// 本文件守三件错了会**静默失效**的事：
//
//  1. 失败 / 成功都渲染**整页提示**（HTTP 200 + data-jump-state="err"|"ok" + 受控文案），
//     而不是 302 + ?err=，也不是 c.String 纯文本；
//  2. htmx 请求走 **HX-Redirect**（htmx 会自己跟随 302，那时响应头已读不到 Location，
//     整页 HTML 会被塞进片段位）；
//  3. 回跳地址由 shell.BackPath / shell.WithParams 从**服务端**拼，筛选上下文按调用点
//     显式列出的键读回 —— 手拼的 ?err= / ?done= 不再被渲染成「系统说的话」。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/templates"
)

// newProductJumpContext 造一个带真实 Jet 渲染器的请求上下文（提示页断言用）。
//
// 用真实渲染器而不是替身：提示页的判据就是响应体里那几处标记（data-jump-state /
// 文案 / 回跳链接），替身渲染器看不到它们。
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

// TestProductListJumpRendersNoticePage 原生失败渲染整页提示（200 + err 态 + 文案 + 回跳链接）。
func TestProductListJumpRendersNoticePage(t *testing.T) {
	c, rec := newProductJumpContext(t, "", "project=p1&keyword=abc&status=draft&page=2", "")
	productListJump(c, false, "商品名称必填")
	c.Writer.WriteHeaderNow()

	if rec.Code != http.StatusOK {
		t.Fatalf("提示页状态码 = %d，want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("失败应渲染 err 态提示页，body=%s", body)
	}
	if !strings.Contains(body, "商品名称必填") {
		t.Fatal("提示页应含受控文案")
	}
	// 回跳链接必须带上本次请求的筛选上下文（表单 action 的 query）。
	if !strings.Contains(body, "/admin/products?") || !strings.Contains(body, "project=p1") {
		t.Fatalf("提示页的回跳链接应保留工程筛选，body=%s", body)
	}
	if !strings.Contains(body, "keyword=abc") || !strings.Contains(body, "page=2") {
		t.Fatalf("提示页的回跳链接应保留关键词与页码，body=%s", body)
	}
}

// TestProductJumpHXRedirect htmx 请求走 HX-Redirect（不把整页 HTML 塞进片段位）。
func TestProductJumpHXRedirect(t *testing.T) {
	c, rec := newProductJumpContext(t, "true", "project=p1", "")
	productListJump(c, true, "操作已完成")
	c.Writer.WriteHeaderNow()

	if rec.Code != http.StatusOK {
		t.Fatalf("htmx 档状态码 = %d，want 200", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); !strings.Contains(got, "/admin/products") || !strings.Contains(got, "project=p1") {
		t.Errorf("htmx 档应发 HX-Redirect 回列表（带筛选），实际 %q", got)
	}
	if strings.Contains(rec.Body.String(), "data-jump-state") {
		t.Error("htmx 档不该把整页 HTML 塞进响应体")
	}
}

// TestProductBackHelpersKeepFilters 各页回跳助手从**本次请求 query** 读回筛选上下文。
//
// 键表与服务两条路径共用一份（渲染时拼进表单 action、POST 回来时读回），这里钉的是
// 读回这一侧：漏一个键的症状是「写完跳回去筛选静默丢了」，不报错也不记日志。
func TestProductBackHelpersKeepFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/x?project=p1&keyword=abc&status=draft&page=2&variation=1&limit=20&product=prod1&lang=en-US&source=BundleSourceProduct", nil)

	cases := []struct {
		name string
		got  string
		want []string
	}{
		{"列表", productListBack(c), []string{"/admin/products?", "project=p1", "keyword=abc", "status=draft", "page=2"}},
		{"属性", productAttrBack(c), []string{"/admin/product-attributes?", "project=p1", "variation=1"}},
		{"标签", productTagsBack(c), []string{"/admin/product-tags?", "project=p1", "keyword=abc"}},
		{"分类", productCategoriesBack(c), []string{"/admin/product-categories?", "project=p1"}},
		{"品牌", productBrandsBack(c), []string{"/admin/product-brands?", "project=p1"}},
		{"捆绑", productBundleBack(c), []string{"/admin/products/bundle?", "project=p1", "product=prod1"}},
		{"定价", productPricingBack(c), []string{"/admin/product-pricing?", "project=p1"}},
		{"翻译", productTranslationsBack(c), []string{"/admin/products/translations?", "project=p1", "product=prod1", "lang=en-US"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range tc.want {
				if !strings.Contains(tc.got, want) {
					t.Errorf("回跳地址 %q 缺 %q", tc.got, want)
				}
			}
		})
	}
	// 不该透传的键（不在白名单里）一个都不带。
	if strings.Contains(productListBack(c), "source=") {
		t.Error("列表回跳不该透传未列出的键 source")
	}
	// 编辑页 / 新建页的 id 随表单提交，用 WithParams 拼（与 BackPath 同一份参数编码）。
	if got := productEditBack("p1", "prod1"); got != "/admin/products/edit?product=prod1&project=p1" {
		t.Errorf("productEditBack = %q", got)
	}
	if got := productNewBack("p1"); got != "/admin/products/new?project=p1" {
		t.Errorf("productNewBack = %q", got)
	}
}

// TestProductWriteHandlersJumpOnValidationFail 校验型失败（不碰 service）也走整页提示。
func TestProductWriteHandlersJumpOnValidationFail(t *testing.T) {
	h := &productPageHandle{}

	t.Run("改商品缺名称", func(t *testing.T) {
		c, rec := newProductJumpContext(t, "", "", "projectId=p1&productId=prod1&name=")
		h.ProductsUpdate(c)
		c.Writer.WriteHeaderNow()
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，want 200（提示页）", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `data-jump-state="err"`) {
			t.Fatalf("应渲染 err 态提示页，body=%s", body)
		}
		if !strings.Contains(body, productErrFallbacks[productenums.ErrNameRequired]) {
			t.Fatalf("提示页应含「商品名称必填」，body=%s", body)
		}
		if !strings.Contains(body, "/admin/products/edit?") || !strings.Contains(body, "product=prod1") {
			t.Fatalf("回跳应回编辑页并带上商品，body=%s", body)
		}
	})

	t.Run("生成变体未勾选", func(t *testing.T) {
		c, rec := newProductJumpContext(t, "", "", "projectId=p1&productId=prod1")
		h.ProductsVariantGenerate(c)
		c.Writer.WriteHeaderNow()
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `data-jump-state="err"`) {
			t.Fatalf("应渲染 err 态提示页，body=%s", rec.Body.String())
		}
	})
}

// TestProductJumpBackTextFollowsPage 回跳链接文字取各页标题词条（不新增全站词条）。
func TestProductJumpBackTextFollowsPage(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "", "")
	if got := productListBackText(c); got == "" {
		t.Error("列表回跳链接文字为空")
	}
	if got := productAttrBackText(c); got == "" {
		t.Error("属性页回跳链接文字为空")
	}
	if got := productPricingBackText(c); got == "" {
		t.Error("定价页回跳链接文字为空")
	}
}

// TestProductBulkDeleteResultSentences 批量结论必须给出非空整句（空串会让提示页显示一句空话）。
func TestProductBulkDeleteResultSentences(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "", "")
	for _, tc := range []struct{ deleted, skipped int }{{0, 0}, {3, 0}, {0, 3}, {2, 3}} {
		_, msg := productBulkDeleteResult(c, tc.deleted, tc.skipped, productTagBulkPartial, productTagBulkDone)
		if strings.TrimSpace(msg) == "" {
			t.Errorf("批量结论为空串（deleted=%d skipped=%d）", tc.deleted, tc.skipped)
		}
	}
	// 单条删除复用批量结论文案（计数 1）：与批量入口措辞一致。
	if got := productBulkDoneText(c, productTagBulkDone); !strings.Contains(got, "1") {
		t.Errorf("单条删除结论 = %q，应含计数 1", got)
	}
}

// TestProductQueryFromRequestDropsEmptyAndUnknown 筛选上下文只透传列出的键、空值丢弃。
func TestProductQueryFromRequestDropsEmptyAndUnknown(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "project=p1&keyword=&status=&source=x", "")
	got := productQueryFromRequest(c, productListBackKeys...)
	if got != "project=p1" {
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

// TestProductJumpBackIsSameOrigin 回跳地址必须是站内相对路径（开放重定向防护）。
//
// 直接调 shell.BackPath 的判据由 shell 包自己守；这里只确认本模块的出口拿到的
// 是站内相对路径（以 / 开头、不带 scheme / host）。
func TestProductJumpBackIsSameOrigin(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "project=p1", "")
	for _, got := range []string{productListBack(c), productAttrBack(c), productEditBack("p1", "prod1")} {
		if !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") {
			t.Errorf("回跳地址应为站内相对路径，实际 %q", got)
		}
	}
}
