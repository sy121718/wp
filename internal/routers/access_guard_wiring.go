package routers

import (
	"strings"

	"github.com/gin-gonic/gin"

	usercontract "go_wp/internal/module/user/contract"
	"go_wp/pkg/auth"
)

const (
	visitorSessionCookieNameMirror = "gowp_user_session"
	visitorSessionTokenKeyMirror   = "user_token"
)

func newVisitorLoginProbe(svc usercontract.UserService) func(*gin.Context) bool {
	if svc == nil {
		return nil
	}

	store, err := auth.NamedCookieStore(visitorSessionCookieNameMirror)
	if err != nil {
		return nil
	}
	return func(c *gin.Context) bool {
		if c == nil || c.Request == nil {
			return false
		}
		sess, serr := store.Get(c.Request, visitorSessionCookieNameMirror)
		if serr != nil {
			return false
		}
		raw, ok := sess.Values[visitorSessionTokenKeyMirror].(string)
		if !ok {
			return false
		}
		token := strings.TrimSpace(raw)
		if token == "" {
			return false
		}

		id, ok := svc.ResolveVisitorID(c.Request.Context(), token)
		return ok && id != 0
	}
}
