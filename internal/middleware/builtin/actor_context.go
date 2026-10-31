package builtin

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/actor"
)

func ActorContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request != nil {
			a := actor.Actor{
				ID:   int64(CurrentUserID(c)),
				Name: OperatorName(c),
			}
			c.Request = c.Request.WithContext(actor.WithActor(c.Request.Context(), a))
		}
		c.Next()
	}
}
