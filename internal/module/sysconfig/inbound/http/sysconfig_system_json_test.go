package sysconfighttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigenums "go_wp/internal/module/sysconfig/enums"
	sysconfighttp "go_wp/internal/module/sysconfig/inbound/http"
)

type systemJSONStub struct {
	sysconfigcontract.Service
	saved   *sysconfigdto.SetGroupsReq
	saveErr error
	dictErr error
}

func (s *systemJSONStub) GetGroup(ctx context.Context, key string) (*sysconfigdto.Group, error) {
	return &sysconfigdto.Group{Key: key, Version: 99, Data: map[string]any{"expert": "retained"}}, nil
}
func (s *systemJSONStub) ListDictOptions(ctx context.Context, kind string) ([]sysconfigdto.DictOption, error) {
	code := "en-US"
	if kind == "currency" {
		code = "USD"
	}
	return []sysconfigdto.DictOption{{Code: code}}, s.dictErr
}
func (s *systemJSONStub) ListCountryOptions(ctx context.Context, lang string) ([]sysconfigdto.CountryOption, error) {
	return []sysconfigdto.CountryOption{{Code: "US"}}, s.dictErr
}
func (s *systemJSONStub) SetGroups(ctx context.Context, req *sysconfigdto.SetGroupsReq) ([]sysconfigdto.Group, error) {
	s.saved = req
	return []sysconfigdto.Group{{Key: "i18n", Version: 8}, {Key: "trade", Version: 10}}, s.saveErr
}
func TestSystemJSONAggregate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, body       string
		saveErr, dictErr error
		status           int
		saved            bool
	}{
		{name: "atomic aggregate", body: `{"defaultLang":"en-US","siteLangURLMode":"off","defaultCountry":"US","defaultCurrency":"USD","i18nVersion":7,"tradeVersion":9}`, status: 200, saved: true},
		{name: "conflict", body: `{"defaultLang":"en-US","siteLangURLMode":"off","defaultCountry":"US","defaultCurrency":"USD","i18nVersion":7,"tradeVersion":9}`, saveErr: errors.New(sysconfigenums.ErrVersionConflict), status: 409, saved: true},
		{name: "unknown option", body: `{"defaultLang":"xx","siteLangURLMode":"off","defaultCountry":"US","defaultCurrency":"USD","i18nVersion":7,"tradeVersion":9}`, status: 400},
		{name: "dictionary fails closed", body: `{"defaultLang":"en-US","siteLangURLMode":"off","defaultCountry":"US","defaultCurrency":"USD","i18nVersion":7,"tradeVersion":9}`, dictErr: errors.New("private SQL error"), status: 500},
		{name: "bad JSON", body: `{"i18nVersion":"7"}`, status: 400},
		{name: "missing version", body: `{"defaultLang":"en-US","siteLangURLMode":"off","defaultCountry":"US","defaultCurrency":"USD"}`, status: 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &systemJSONStub{saveErr: tc.saveErr, dictErr: tc.dictErr}
			h := sysconfighttp.NewAdminHandle(stub)
			engine := gin.New()
			engine.POST("/api/sysconfig/system/save", func(c *gin.Context) { c.Set("user_id", uint64(42)); h.APISystemSave(c) })
			req := httptest.NewRequest(http.MethodPost, "/api/sysconfig/system/save", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private SQL") {
				t.Fatal("internal error leaked")
			}
			if (stub.saved != nil) != tc.saved {
				t.Fatalf("saved=%v", stub.saved)
			}
			if stub.saved != nil {
				if len(stub.saved.Groups) != 2 || stub.saved.Groups[0].Version != 7 || stub.saved.Groups[1].Version != 9 {
					t.Fatalf("submitted versions lost: %+v", stub.saved)
				}
				for _, g := range stub.saved.Groups {
					if g.Data["expert"] != "retained" {
						t.Fatal("unknown key lost")
					}
				}
			}
		})
	}
}
