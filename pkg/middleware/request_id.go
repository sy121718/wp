package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
)

const ContextKeyRequestID = "request_id"

const HeaderRequestID = "X-Request-Id"

const requestIDMaxLen = 64

func NewRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {

		return "noentropy"
	}
	return hex.EncodeToString(buf[:])
}

func sanitizeRequestID(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > requestIDMaxLen {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c == '_' || c == '.' || c == ':' || c == '-':
		default:
			return "", false
		}
	}
	return s, true
}

func RequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(ContextKeyRequestID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := sanitizeRequestID(c.GetHeader(HeaderRequestID))
		if !ok {
			id = NewRequestID()
		}

		c.Request.Header.Set(HeaderRequestID, id)

		c.Header(HeaderRequestID, id)
		c.Set(ContextKeyRequestID, id)

		c.Request = c.Request.WithContext(logger.WithRequestID(c.Request.Context(), id))
		c.Next()
	}
}

func RequestIDFromContext(ctx context.Context) string {
	return logger.RequestIDFrom(ctx)
}
