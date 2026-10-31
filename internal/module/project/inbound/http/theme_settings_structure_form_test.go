package projecthttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStructureTemplateFormValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name    string
		form    url.Values
		key     string
		current string
		want    string
	}{
		{
			name: "带哨兵 + 提交空值 = 主动解绑",
			form: url.Values{"structureTemplateFields": {"1"}, "headerTemplateId": {""}},
			key:  "headerTemplateId", current: "tpl-old", want: "",
		},
		{
			name: "带哨兵 + 提交新值 = 切换",
			form: url.Values{"structureTemplateFields": {"1"}, "headerTemplateId": {"tpl-new"}},
			key:  "headerTemplateId", current: "tpl-old", want: "tpl-new",
		},
		{
			name: "不带哨兵（旧表单 / 缺字段）= 保持原值，不清空",
			form: url.Values{},
			key:  "headerTemplateId", current: "tpl-old", want: "tpl-old",
		},
		{
			name: "不带哨兵但带了值 = 仍采用提交值（部分表单 / 脚本提交）",
			form: url.Values{"headerTemplateId": {"tpl-manual"}},
			key:  "headerTemplateId", current: "tpl-old", want: "tpl-manual",
		},
		{
			name: "带哨兵 + 空白 = 解绑（避免只删空格却当成有效绑定）",
			form: url.Values{"structureTemplateFields": {"1"}, "footerTemplateId": {"   "}},
			key:  "footerTemplateId", current: "tpl-footer", want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			req := httptest.NewRequest(http.MethodPost, "/admin/themes/settings/save", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			c.Request = req
			if got := structureTemplateFormValue(c, tc.key, tc.current); got != tc.want {
				t.Fatalf("structureTemplateFormValue(%q) = %q，期望 %q", tc.key, got, tc.want)
			}
		})
	}
}
