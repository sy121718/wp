package orderhttp

import (
	"html"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"go_wp/internal/web/shell"
)

// TestReturnsEmptyFilterActions verifies that empty-list actions navigate to
// the actual status counter URL, preserving independent filters.
func TestReturnsEmptyFilterActions(t *testing.T) {
	tests := []struct {
		name    string
		filter  returnFilter
		wantURL []string
	}{
		{
			name:    "status only keeps keyword and project",
			filter:  returnFilter{Status: returnStatusRequested, Keyword: "mail@example.test"},
			wantURL: []string{"/admin/returns?keyword=mail%40example.test&project=p1"},
		},
		{
			name:   "order and status offer independent clearing",
			filter: returnFilter{Status: returnStatusRequested, Keyword: "search", OrderID: 42},
			wantURL: []string{
				"/admin/returns?keyword=search&project=p1&status=requested",
				"/admin/returns?keyword=search&orderId=42&project=p1",
			},
		},
		{
			name:    "order only keeps existing order clearing",
			filter:  returnFilter{OrderID: 42},
			wantURL: []string{"/admin/returns?project=p1"},
		},
		{
			name:   "unfiltered list has no clearing action",
			filter: returnFilter{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := returnBulkPageData()
			d["Rows"] = []any{}
			d["Statuses"] = returnStatusCounters(nil, tt.filter, "p1")
			d["FilterStatus"] = tt.filter.Status
			d["FilterLabel"] = returnStatusLabel(tt.filter.Status)
			d["FilterKeyword"] = tt.filter.Keyword
			d["FilterOrderID"] = returnOrderIDText(tt.filter.OrderID)
			d["ClearOrderURL"] = shell.FilterBaseURL("/admin/returns", returnFilterValues("p1", returnFilter{
				Status: tt.filter.Status, Keyword: tt.filter.Keyword,
			}))
			out := renderAdminTemplate(t, "admin/order/returns.html", d)
			if !strings.Contains(out, "</html>") || !strings.Contains(out, `colspan="8"`) {
				t.Fatal("empty list must render a complete page and preserve the table header")
			}
			body := out[strings.Index(out, `<tbody>`):]
			links := regexp.MustCompile(`(?s)<div class="empty-actions">(.*?)</div>`).FindStringSubmatch(body)
			var got []string
			if len(links) > 1 {
				for _, match := range regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(links[1], -1) {
					got = append(got, html.UnescapeString(match[1]))
				}
			}
			if !reflect.DeepEqual(got, tt.wantURL) {
				t.Fatalf("empty-state action URLs = %q, want %q", got, tt.wantURL)
			}
		})
	}
}
