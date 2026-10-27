package builtin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/actor"
)

// ActorContextMiddleware 的两件事：已登录时带上 id 与登录名；未登录时是零值（系统身份）。
func TestActorContextMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name     string
		setID    bool
		username string
		wantID   int64
		wantName string
	}{
		{name: "未登录", setID: false, wantID: 0, wantName: ""},
		{name: "已登录", setID: true, username: "ops", wantID: 42, wantName: "ops"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got actor.Actor
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tc.setID {
					c.Set("user_id", int64(42))
					c.Set("username", tc.username)
				}
				c.Next()
			}, ActorContextMiddleware())
			r.GET("/x", func(c *gin.Context) {
				got = actor.From(c.Request.Context())
				c.Status(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
			if w.Code != http.StatusNoContent {
				t.Fatalf("状态码 %d", w.Code)
			}
			if got.ID != tc.wantID || got.Name != tc.wantName {
				t.Fatalf("发起人 = %+v，期望 id=%d name=%q", got, tc.wantID, tc.wantName)
			}
		})
	}
}
