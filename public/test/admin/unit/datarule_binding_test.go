// 数据规则配置的请求形状校验（dto 的 binding tag）。
//
// 这里直接驱动 gin 的 JSON 绑定器 —— 不建路由、不碰 DB：断言的是「声明本身有效」。
// tag 写错（oneof 漏项、dive 漏写）不会有编译错误，只会在收到脏配置时静默放行，
// 所以单独钉一遍；本包其余用例走的是 service 层的域白名单判定。
package unit

import (
	"net/http/httptest"
	"strings"
	"testing"

	admindto "go_wp/internal/module/admin/dto"

	"github.com/gin-gonic/gin/binding"
)

// TestRuleConfigBinding 合法配置通过、结构非法配置在进入 service 之前被拒。
func TestRuleConfigBinding(t *testing.T) {
	accept := []struct{ name, body string }{
		{"空配置", `{"rule_name":"r","domain":"ADMIN","config":{},"status":1}`},
		{"完整配置", `{"rule_name":"r","domain":"ADMIN","config":{"omit_fields":["phone"],"condition_groups":[{"logic":"OR","conditions":[{"field":"status","op":"IN","value":"1,2"}]}]},"status":1}`},
	}
	for _, c := range accept {
		t.Run("通过/"+c.name, func(t *testing.T) {
			if err := bindRuleCreate(c.body); err != nil {
				t.Fatalf("合法配置应通过绑定: %v", err)
			}
		})
	}

	reject := []struct{ name, body string }{
		{"条件组缺 logic", `{"rule_name":"r","domain":"ADMIN","config":{"condition_groups":[{"conditions":[{"field":"status","op":"EQ"}]}]},"status":1}`},
		{"logic 取值非法", `{"rule_name":"r","domain":"ADMIN","config":{"condition_groups":[{"logic":"XOR","conditions":[{"field":"status","op":"EQ"}]}]},"status":1}`},
		{"op 取值非法", `{"rule_name":"r","domain":"ADMIN","config":{"condition_groups":[{"logic":"AND","conditions":[{"field":"status","op":"CONTAINS"}]}]},"status":1}`},
		{"条件缺 field", `{"rule_name":"r","domain":"ADMIN","config":{"condition_groups":[{"logic":"AND","conditions":[{"op":"EQ"}]}]},"status":1}`},
		{"condition_groups 元素不是对象", `{"rule_name":"r","domain":"ADMIN","config":{"condition_groups":["x"]},"status":1}`},
		{"domain 超长", `{"rule_name":"r","domain":"` + strings.Repeat("A", 51) + `","config":{},"status":1}`},
	}
	for _, c := range reject {
		t.Run("拒绝/"+c.name, func(t *testing.T) {
			if err := bindRuleCreate(c.body); err == nil {
				t.Fatalf("非法配置应被绑定层拒绝: %s", c.body)
			}
		})
	}
}

// bindRuleCreate 用 gin 的 JSON 绑定器解析并校验一次 RuleCreateReq。
func bindRuleCreate(body string) error {
	req := httptest.NewRequest("POST", "/api/admin/datarule/create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	var target admindto.RuleCreateReq
	return binding.JSON.Bind(req, &target)
}
