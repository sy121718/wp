package pagehttp

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRedirectPageDataCarriesCountsNotTones(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/api/page/redirect", nil)

	data := redirectPageData(c, nil, "")

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

func TestTranslationRowCarriesEngineNotTone(t *testing.T) {
	typ := reflect.TypeOf(TranslationRowResp{})
	if _, ok := typ.FieldByName("Engine"); !ok {
		t.Fatal("TranslationRowResp.Engine 是模板选档依据的事实，不能删")
	}
	if _, ok := typ.FieldByName("EngineTone"); ok {
		t.Fatal("TranslationRowResp.EngineTone 又回来了：档位由模板按 r.Engine 选（internal/templates/admin/page/page_translations.html:154）")
	}
}
