package pagehttp

// page_tone_facing_test.go — 展示档位不进投影的反向回归（task-113 批次 A）。
//
// 手法与 site_slot_facing_test.go 的 task-103 同源：档位（徽标 class 的语义值）由模板按事实选，
// Go 只交事实。正向渲染用例（public/test/page/feature/page_tone_presentation_test.go）证明
// 「模板按事实选档选得对」；这条反向证明「Go 没有偷偷把档位再算一遍」——
// 少了它，tone 被加回投影时页面照样渲染，两份判据并存、迟早不一致。

import (
	pagedto "go_wp/internal/module/page/dto"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRedirectPageDataCarriesCountsNotTones 重定向统计的投影里只有计数事实。
func TestRedirectPageDataCarriesCountsNotTones(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/page/redirect", nil)

	data := redirectPageData(c, nil, "")

	// 视图换强类型之后用反射检查字段：判据从「map 里有没有这个键」变成
	// 「这个类型有没有这个字段」—— 前者拼错键名静默通过，后者拼错是编译错误。
	typ := reflect.TypeOf(*data)
	for _, field := range []string{"MultiHopCount", "LoopCount"} {
		if _, ok := typ.FieldByName(field); !ok {
			t.Fatalf("投影里缺少计数事实 %q：模板选不出档", field)
		}
	}
	for _, field := range []string{"MultiHopTone", "LoopTone"} {
		if _, ok := typ.FieldByName(field); ok {
			t.Fatalf("投影里又出现了分档 %q：档位由模板按计数选（internal/templates/admin/page/page_redirects.html）", field)
		}
	}
}

// TestTranslationRowCarriesEngineNotTone 译文行只交 engine 事实，字段名即契约（反射：删字段/改名都会红）。
func TestTranslationRowCarriesEngineNotTone(t *testing.T) {
	typ := reflect.TypeOf(pagedto.TranslationRowResp{})
	if _, ok := typ.FieldByName("Engine"); !ok {
		t.Fatal("pagedto.TranslationRowResp.Engine 是模板选档依据的事实，不能删")
	}
	if _, ok := typ.FieldByName("EngineTone"); ok {
		t.Fatal("pagedto.TranslationRowResp.EngineTone 又回来了：档位由模板按 r.Engine 选（internal/templates/admin/page/page_translations.html:154）")
	}
}
