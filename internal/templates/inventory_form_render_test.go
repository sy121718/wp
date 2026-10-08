package templates

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInventoryFailedFormFragmentsRender(t *testing.T) {
	renderer := NewJetHTMLRender(".", true)
	for _, tc := range []struct {
		name   string
		fields map[string]any
	}{
		{"warehouse", map[string]any{"projectId": "p1", "id": "w1", "code": "  WH  ", "name": "仓库", "sort": "", "type": "third_party", "provider": "", "externalCode": "", "address": "", "contact": "", "allowsShipping": "1", "apiCredential": "secret", "secretRef": "", "status": "disabled"}},
		{"reason", map[string]any{"projectId": "p1", "code": "  reason  ", "name": "原因", "direction": "out", "sort": ""}},
		{"source", map[string]any{"projectId": "p1", "id": "s1", "code": "  SRC  ", "name": "货源", "type": "internal", "relatedParty": "false", "status": "disabled", "settlePrice": "", "sort": "", "config": "  {bad}  "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{
				"csrf_token": "csrf", "t": func(_, fallback string) string { return fallback },
				"IsEdit": tc.name != "reason", "Row": map[string]any{}, "FormEcho": tc.fields,
				"SubmitErr": "校验失败", "SelectedProject": "p1",
				// 写表单 action 上带的筛选上下文（handler 的 fail 分支一定给；测试补齐）。
				"ListQuery":      "",
				"WarehouseTypes": []map[string]any{{"Value": "third_party", "Label": "第三方仓"}},
				"Directions":     []map[string]any{{"Value": "out", "Label": "出库"}},
				"TypeOptions":    []map[string]any{{"Value": "internal", "Label": "内部"}},
				"RelatedOptions": []map[string]any{{"Value": "false", "Label": "非关联方"}},
				"StatusOptions":  []map[string]any{{"Value": "disabled", "Label": "停用"}},
			}
			rec := httptest.NewRecorder()
			err := renderer.Instance("admin/inventory/inventory_"+tc.name+"_form.html", data).Render(rec)
			if err != nil {
				t.Fatal(err)
			}
			html := rec.Body.String()
			if !strings.Contains(html, "校验失败") || !strings.Contains(html, `value="  `) || !strings.Contains(html, `hx-swap="outerHTML"`) {
				t.Fatalf("失败片段丢错误、原值或宿主: %s", html)
			}
		})
	}
}
