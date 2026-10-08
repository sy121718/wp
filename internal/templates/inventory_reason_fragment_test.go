package templates

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInventoryReasonEditFailureFragmentRenders(t *testing.T) {
	renderer := NewJetHTMLRender(".", true)
	for _, tc := range []struct {
		name, builtin string
		wantName      bool
	}{
		{"builtin", "1", false},
		{"custom", "0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := map[string]any{
				"csrf_token": "csrf", "t": func(_, fallback string) string { return fallback },
				"Edit": true, "SelectedProject": "p1", "SubmitErr": "校验失败",
				"ListQuery": "",
				"FormEcho":  map[string]any{"projectId": "p1", "id": "7", "builtin": tc.builtin, "name": "  用户原值  ", "sort": " 9 ", "status": ""},
			}
			rec := httptest.NewRecorder()
			if err := renderer.Instance("admin/inventory/inventory_reason_form.html", data).Render(rec); err != nil {
				t.Fatal(err)
			}
			body := rec.Body.String()
			for _, expected := range []string{"校验失败", `value=" 9 "`, `name="id"`, `hx-post="/admin/inventory/reason/update?`} {
				if !strings.Contains(body, expected) {
					t.Errorf("missing %q: %s", expected, body)
				}
			}
			if got := strings.Contains(body, `name="name"`); got != tc.wantName {
				t.Errorf("name field visible=%v, want %v: %s", got, tc.wantName, body)
			}
			if tc.wantName && !strings.Contains(body, `value="  用户原值  "`) {
				t.Error("custom name lost original input")
			}
		})
	}
}
