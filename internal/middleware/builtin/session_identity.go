package builtin

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func CurrentUserID(c *gin.Context) uint64 {
	if id := GetUserID(c); id > 0 {
		return uint64(id)
	}
	return 0
}

func CurrentUserIDText(c *gin.Context) string {
	id := CurrentUserID(c)
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

func OperatorName(c *gin.Context) string {
	if name := strings.TrimSpace(GetUsername(c)); name != "" {
		return name
	}
	value, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	case uint64:
		return strconv.FormatUint(v, 10)
	}
	return ""
}
