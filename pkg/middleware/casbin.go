package middleware

import (
	"net/http"
	"strconv"

	"go_wp/pkg/casbin"
	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/view"

	"github.com/gin-gonic/gin"
)

func CasbinMiddleware() gin.HandlerFunc {
	return casbinMiddleware("", "")
}

func CasbinMiddlewareForPath(obj string) gin.HandlerFunc {
	return casbinMiddleware(obj, "")
}

func CasbinMiddlewareForPathAs(obj, act string) gin.HandlerFunc {
	if act != "" && !validHTTPMethod(act) {
		panic("CasbinMiddlewareForPathAs 收到非法 HTTP 方法：" + act + "（期望 GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS）")
	}
	return casbinMiddleware(obj, act)
}

func validHTTPMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

func casbinMiddleware(forcedObj, forcedAct string) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, key := CasbinDecide(c, forcedObj, forcedAct)
		if status != 0 {
			view.FailStatus(c, status, "middleware", facing.New(key))
			c.Abort()
			return
		}
		c.Next()
	}
}

func CasbinDecide(c *gin.Context, obj, act string) (status int, key string) {
	userID := GetUserID(c)
	if userID <= 0 {
		return http.StatusUnauthorized, enums.ErrUserInfoMissing
	}
	if obj == "" {
		obj = c.Request.URL.Path
	}
	if act == "" {
		act = c.Request.Method
	}
	sub := strconv.FormatInt(userID, 10)

	enforcer := casbin.GetEnforcer()
	if enforcer == nil {
		return http.StatusInternalServerError, enums.ErrAuthzNotReady
	}

	ok, err := enforcer.Enforce(sub, obj, act)
	if err != nil {
		logger.Scene("middleware").With("sub", sub).With("obj", obj).With("act", act).Error(err, "鉴权失败")
		return http.StatusInternalServerError, enums.ErrAuthzFailed
	}
	if !ok {
		logger.Scene("middleware").With("sub", sub).With("obj", obj).With("act", act).Warn("鉴权失败")
		return http.StatusForbidden, enums.ErrPermissionDenied
	}
	return 0, ""
}

func GetUserID(c *gin.Context) int64 {
	v, exists := c.Get("user_id")
	if !exists {
		return 0
	}
	id, ok := v.(int64)
	if !ok {
		return 0
	}
	return id
}

func GetUsername(c *gin.Context) string {
	v, exists := c.Get("username")
	if !exists {
		return ""
	}
	name, ok := v.(string)
	if !ok {
		return ""
	}
	return name
}
