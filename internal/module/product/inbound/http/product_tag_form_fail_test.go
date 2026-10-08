package producthttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// tagRuleTypesStub 只实现片段数据需要的只读契约，其余接口靠嵌入接口零值 panic 即暴露误用。
type tagRuleTypesStub struct {
	productcontract.ProductService
	types []*productdto.TagRuleTypeResp
}

func (s *tagRuleTypesStub) ListTagRuleTypes(ctx context.Context) []*productdto.TagRuleTypeResp {
	return s.types
}

func tagRuleTypesForTest() []*productdto.TagRuleTypeResp {
	return []*productdto.TagRuleTypeResp{
		{Type: "new_arrival", Name: "新到商品", Params: "days：1~365 的整数"},
		{Type: "price_range", Name: "价格区间", Params: "minPrice / maxPrice"},
	}
}

// TestTagDrawerFailureEcho 标签写失败必须原样回填全部字段：
// 每个字段的值逐项比对（含首尾空格与空串），错误槽在片段数据里且不泄漏裸 key。
func TestTagDrawerFailureEcho(t *testing.T) {
	h := &productPageHandle{products: &tagRuleTypesStub{types: tagRuleTypesForTest()}}
	form := url.Values{"projectId": {"p1"}, "id": {"t1"}, "name": {"  促销中  "}, "slug": {""},
		"kind": {"rule"}, "sort": {"03"}, "ruleType": {"new_arrival"}, "days": {"7"},
		"minPrice": {""}, "maxPrice": {"199.00"}}
	for _, mode := range []string{"create", "update"} {
		t.Run(mode, func(t *testing.T) {
			c, rec, cap := newAttrCaptureContext(t, "true", form.Encode())
			h.tagFormFail(c, mode, errors.New(productenums.ErrTagSlugTaken))
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK || cap.name != "admin/product/product_tag_form.html" {
				t.Fatalf("failure response: status=%d template=%q", rec.Code, cap.name)
			}
			data := capturedData(t, cap)
			if data["SubmitErr"] == "" || data["SubmitErr"] == productenums.ErrTagSlugTaken {
				t.Fatalf("error slot missing or bare key leaked: %v", data["SubmitErr"])
			}
			echo := data["FormEcho"].(gin.H)
			for _, key := range []string{"projectId", "name", "slug", "kind", "sort", "ruleType", "days", "minPrice", "maxPrice"} {
				if echo[key] != form.Get(key) {
					t.Errorf("%s: got %q want %q", key, echo[key], form.Get(key))
				}
			}
			if mode == "update" && echo["id"] != "t1" {
				t.Errorf("id: got %q want %q", echo["id"], "t1")
			}
		})
	}
}

// TestTagDrawerNativeFailureAndSuccess 原生失败渲染整页提示（取代原先的 302 + ?err=）；
// 成功两档：htmx 走 HX-Redirect（XHR 会跟随 302，读不到 Location），原生渲染 ok 态提示页。
func TestTagDrawerNativeFailureAndSuccess(t *testing.T) {
	h := &productPageHandle{products: &tagRuleTypesStub{types: tagRuleTypesForTest()}}
	c, rec := newProductJumpContext(t, "", "project=p1", "projectId=p1&name=Draft")
	h.tagFormFail(c, "create", errors.New(productenums.ErrTagSlugTaken))
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-jump-state="err"`) {
		t.Fatalf("native failure: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/admin/product-tags?") || !strings.Contains(rec.Body.String(), "project=p1") {
		t.Fatalf("native failure 回跳应回标签页并带上工程，body=%s", rec.Body.String())
	}
	for _, hx := range []string{"true", ""} {
		if hx == "true" {
			c, rec, _ := newAttrCaptureContext(t, hx, "projectId=p1")
			tagFormSuccess(c, "p1")
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("HX-Redirect"), "/admin/product-tags") {
				t.Errorf("HX success: %d headers=%v", rec.Code, rec.Header())
			}
			continue
		}
		c, rec := newProductJumpContext(t, "", "project=p1", "projectId=p1")
		tagFormSuccess(c, "p1")
		c.Writer.WriteHeaderNow()
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
			t.Errorf("native success: %d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
		}
	}
}

// TestTagTemplateEchoFieldsMatchFragment 片段模板引用的回填键必须都在
// handler 的清单里（漏列 = 渲染 500 + htmx 不 swap，用户点了保存毫无反应）；
// 反向：清单里的关键键模板必须真的在读，否则清单在悄悄漂移。
func TestTagTemplateEchoFieldsMatchFragment(t *testing.T) {
	fields := []string{"projectId", "id", "name", "slug", "kind", "sort", "ruleType", "days", "minPrice", "maxPrice"}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "templates", "admin", "product", "product_tag_form.html"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\.FormEcho\.([a-zA-Z][a-zA-Z0-9]*)`)
	actual := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		actual[m[1]] = true
	}
	want := map[string]bool{}
	for _, f := range fields {
		want[f] = true
	}
	for key := range actual {
		if !want[key] {
			t.Errorf("模板引用未列入回填清单: %s", key)
		}
	}
	for _, key := range []string{"name", "slug", "kind", "sort", "ruleType"} {
		if !actual[key] {
			t.Errorf("回填清单字段 %s 在模板中未被引用", key)
		}
	}
}
