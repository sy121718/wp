package reqparams

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func IsHXRequest(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true")
}

func FieldValue(c *gin.Context, key string) string {
	return strings.TrimSpace(c.PostForm(key))
}

func ParseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

func ParseStatus(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func ParseStatusPtr(s string) *int {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	v, _ := strconv.Atoi(t)
	return &v
}

func FilterBaseURL(path string, filters map[string]string) string {
	q := url.Values{}
	for k, v := range filters {
		if v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

func PageParams(c *gin.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.Query("page"))
	limit, _ = strconv.Atoi(c.Query("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
