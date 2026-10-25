package orderhttp

import (
	"net/http/httptest"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

func TestLocalizeReturnLabelsUsesRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{orderenums.ReturnStatusKeyRequested: {"en-US": "Awaiting review", "zh-CN": "待审核"}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	for _, tc := range []struct{ lang, status, want string }{
		{"en-US", "requested", "Awaiting review"},
		{"zh-CN", "requested", "待审核"},
		{"en-US", "approved", "待收货"},
		{"en-US", "", "—"},
		{"en-US", "unknown", "unknown"},
	} {
		t.Run(tc.lang+"/"+tc.status, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/probe?lang="+tc.lang, nil)
			r := &orderdto.ReturnResp{Status: tc.status}
			localizeReturnLabels(c, nil, r)
			if r.StatusLabel != tc.want {
				t.Fatalf("label=%q, want %q", r.StatusLabel, tc.want)
			}
		})
	}
}
