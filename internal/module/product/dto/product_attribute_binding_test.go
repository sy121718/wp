package productdto_test

import (
	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// 显式表单入口（ParseForm）与显式 JSON 入口各自只读声明的那一种形状：
// 表单经 DTO 的 BindForm 钩子，JSON 走标准解码，两者解出同一份事实。
func TestAttributeValuesFormAndJSONBindSameRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jsonBody := `{"id":"a1","projectId":"p1","values":[{"id":"v1","key":"red","label":"红","sort":2,"enabled":false}]}`
	formBody := url.Values{"id": {"a1"}, "projectId": {"p1"}, "values": {`[{"id":"v1","key":"red","label":"红","sort":"2","enabled":"false"}]`}}.Encode()
	var requests []*productdto.SetAttributeValuesReq
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")
	jsonReq, err := binding.ParseJSON[productdto.SetAttributeValuesReq](c)
	if err != nil {
		t.Fatalf("JSON 绑定失败: %v", err)
	}
	requests = append(requests, jsonReq)

	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(formBody))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formReq, err := binding.ParseForm[productdto.SetAttributeValuesReq](c)
	if err != nil {
		t.Fatalf("表单绑定失败: %v", err)
	}
	requests = append(requests, formReq)
	if !reflect.DeepEqual(requests[0], requests[1]) {
		t.Fatalf("JSON 与 FormData 事实不同: %+v / %+v", requests[0], requests[1])
	}
	if requests[1].Values[0].Enabled == nil || *requests[1].Values[0].Enabled {
		t.Fatal("未勾选必须显式保留 false")
	}
}

func TestAttributeValuesFormRejectsInvalidAndKeepsEmptyArray(t *testing.T) {
	for _, raw := range []string{`{}`, `[{"sort":"bad"}]`, `[{"enabled":"maybe"}]`} {
		req := &productdto.SetAttributeValuesReq{}
		if err := req.BindForm(url.Values{"values": {raw}}); err == nil {
			t.Fatalf("非法输入被接受: %s", raw)
		}
	}
	req := &productdto.SetAttributeValuesReq{}
	if err := req.BindForm(url.Values{"values": {"[]"}}); err != nil || req.Values == nil || len(req.Values) != 0 {
		t.Fatalf("清空语义丢失: %+v %v", req.Values, err)
	}
}

func TestAttributeVariationFormFollowsCheckboxFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	binders := []struct {
		name string
		bind func(*gin.Context) (*bool, bool)
	}{
		{"create", func(c *gin.Context) (*bool, bool) {
			req, err := binding.ParseForm[productdto.CreateAttributeReq](c)
			if err != nil {
				return nil, false
			}
			return req.IsVariation, true
		}},
		{"update", func(c *gin.Context) (*bool, bool) {
			req, err := binding.ParseForm[productdto.UpdateAttributeReq](c)
			if err != nil {
				return nil, false
			}
			return req.IsVariation, true
		}},
	}
	for _, binder := range binders {
		for _, tc := range []struct {
			name   string
			values []string
			want   string
		}{
			{"checked", []string{"0", "1"}, "true"},
			{"unchecked", []string{"0"}, "false"},
			{"omitted", nil, "nil"},
			{"invalid", []string{"0", "not-a-boolean"}, "error"},
		} {
			t.Run(binder.name+"/"+tc.name, func(t *testing.T) {
				form := url.Values{"id": {"a1"}, "projectId": {"p1"}, "name": {"颜色"}}
				if tc.values != nil {
					form["isVariation"] = tc.values
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
				c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				got, ok := binder.bind(c)
				if tc.want == "error" {
					if ok {
						t.Fatal("非法末值被接受")
					}
					return
				}
				if !ok {
					t.Fatal("属性开关绑定失败")
				}
				if tc.want == "nil" {
					if got != nil {
						t.Fatal("省略开关不应变成 false 写入")
					}
					return
				}
				if got == nil || *got != (tc.want == "true") {
					t.Fatalf("开关绑定=%v，预期%s", got, tc.want)
				}
			})
		}
	}
}
