package runtimefragment

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 清退能力必须从注册表和实际入口一起消失，不能只删模板留下可调用的空壳。
func TestRetiredCommentAndMembershipCapabilitiesAreUnavailable(t *testing.T) {
	router := newRouter()
	for _, name := range []string{"commentList", "commentSubmit", "membershipBadge", "membershipPanel"} {
		if _, ok := Lookup(name); ok {
			t.Fatalf("已清退能力仍注册: %s", name)
		}
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/_fragments/"+name, strings.NewReader("projectId=33333333-3333-4333-8333-333333333333&body=不应写入"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s应不可调用，status=%d body=%s", method, name, rec.Code, rec.Body.String())
			}
		}
	}
}
