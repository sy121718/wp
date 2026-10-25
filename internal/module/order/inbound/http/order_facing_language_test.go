package orderhttp

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

func TestOrderFacingErrorsUseRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ErrCouponNotFound: {"en-US": "Coupon missing", "zh-CN": "优惠码不存在"},
		orderenums.ErrReturnNotFound: {"en-US": "Return missing", "zh-CN": "退货单不存在"},
		adminui.MsgInternalError:     {"en-US": "Internal failure", "zh-CN": "内部错误"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	for _, tc := range []struct {
		name, key, english, chinese string
		facing                      func(*gin.Context, error) string
	}{
		{"order", orderenums.ErrCouponNotFound, "Coupon missing", "优惠码不存在", orderFacingError},
		{"coupon", orderenums.ErrCouponNotFound, "Coupon missing", "优惠码不存在", couponFacingError},
		{"return", orderenums.ErrReturnNotFound, "Return missing", "退货单不存在", returnFacingError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, lang := range []string{"en-US", "zh-CN"} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("GET", "/probe?lang="+lang, nil)
				want, internal := tc.english, "Internal failure"
				if lang == "zh-CN" {
					want, internal = tc.chinese, "内部错误"
				}
				if got := tc.facing(c, nil); got != "" {
					t.Fatalf("nil error = %q", got)
				}
				if got := tc.facing(c, errors.New(tc.key)); got != want {
					t.Fatalf("%s: got %q, want %q", lang, got, want)
				}
				if got := tc.facing(c, errors.New("private SQLSTATE 42P01")); got != internal || strings.Contains(got, "SQLSTATE") {
					t.Fatalf("internal error = %q", got)
				}
			}
		})
	}
}
