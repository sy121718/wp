package navigationhttp

import (
	"bytes"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	json "go_wp/pkg/jsonx"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

const (
	navigationWriteMaxBodyBytes = 1 << 20
)

func navigationWriteBody(c *gin.Context) ([]byte, map[string]json.RawMessage, bool) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, navigationWriteMaxBodyBytes+1))
	if err != nil {
		logger.Scene("navigation").Error(err, "读取导航写请求体失败")
		response.ParamError(c)
		return nil, nil, false
	}
	if len(body) > navigationWriteMaxBodyBytes {

		logger.Scene("navigation").With("bytes", len(body)).Warn("导航写请求体超过上限，已拒绝")
		response.ParamError(c)
		return nil, nil, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		response.ParamError(c)
		return nil, nil, false
	}
	return body, raw, true
}

func navigationWriteAction(raw map[string]json.RawMessage) (string, bool, bool) {
	value, exists := raw["action"]
	if !exists {
		return "", false, true
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", true, false
	}
	var action string
	if err := json.Unmarshal(value, &action); err != nil {
		return "", true, false
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return "", true, false
	}
	return action, true, true
}

func navigationWriteRequiredString(raw map[string]json.RawMessage, key string) (string, bool) {
	value, ok := raw[key]
	if !ok {
		return "", false
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}
