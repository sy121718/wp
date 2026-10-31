package orderhttp

import (
	"bytes"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/internal/middleware/builtin"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

const orderWriteScene = "order"

const orderWriteMaxBodyBytes = 1 << 20

func orderWriteBody(c *gin.Context) (map[string]json.RawMessage, bool) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return orderWriteReject(c, "写请求缺少请求体")
	}
	body, ok := orderWriteReadBody(c)
	if !ok {
		return nil, false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	if orderWriteIsFormBody(c) {
		return orderWriteFormRaw(c, body)
	}

	if c.ContentType() != ginbinding.MIMEJSON {
		return nil, true
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return orderWriteReject(c, "写请求体不是 JSON 对象")
	}
	return raw, true
}

func orderWriteReject(c *gin.Context, reason string) (map[string]json.RawMessage, bool) {
	logger.Scene(orderWriteScene).With("reason", reason).Warn("订单写请求体被拒")
	response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
	return nil, false
}

func orderWriteReadBody(c *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, orderWriteMaxBodyBytes+1))
	if err != nil {
		logger.Scene(orderWriteScene).Error(err, "读取订单写请求体失败")
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return nil, false
	}
	if len(body) > orderWriteMaxBodyBytes {

		logger.Scene(orderWriteScene).With("bytes", len(body)).Warn("订单写请求体超过上限，已拒绝")
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return nil, false
	}
	return body, true
}

func orderWriteIsFormBody(c *gin.Context) bool {
	ct := c.ContentType()
	return strings.HasPrefix(ct, "application/x-www-form-urlencoded") ||
		strings.HasPrefix(ct, "multipart/form-data")
}

func orderWriteFormRaw(c *gin.Context, body []byte) (map[string]json.RawMessage, bool) {
	if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(orderWriteMaxBodyBytes); err != nil {
			return orderWriteReject(c, "multipart 请求体解析失败")
		}
	} else if err := c.Request.ParseForm(); err != nil {
		return orderWriteReject(c, "表单请求体解析失败")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	form := c.Request.PostForm
	if len(form) == 0 {
		return nil, true
	}
	raw := make(map[string]json.RawMessage, len(form))
	for key, vals := range form {
		if len(vals) == 0 {
			continue
		}
		raw[key] = formValueRaw(key, vals)
	}
	if len(raw) == 0 {
		return nil, true
	}
	return raw, true
}

func formValueRaw(key string, vals []string) json.RawMessage {
	if key == "ids" || key == "ids[]" {
		return quotedJSONArray(vals)
	}
	if len(vals) > 1 {
		return quotedJSONArray(vals)
	}
	return rawScalarOf(vals[0])
}

func quotedJSONArray(vals []string) json.RawMessage {
	quoted := make([]string, 0, len(vals))
	for _, v := range vals {
		quoted = append(quoted, strconv.Quote(v))
	}
	return json.RawMessage("[" + strings.Join(quoted, ",") + "]")
}

func rawScalarOf(v string) json.RawMessage {
	switch v {
	case "true", "false", "null":
		return json.RawMessage(v)
	case "":
		return json.RawMessage(`""`)
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return json.RawMessage(v)
	}
	return json.RawMessage(strconv.Quote(v))
}

func orderWriteHasKey(raw map[string]json.RawMessage, key string) bool {
	_, ok := raw[key]
	return ok
}

func orderWriteShapeOK(raw map[string]json.RawMessage, allow map[string]bool) bool {
	for key := range raw {
		if !allow[key] {
			return false
		}
	}
	return true
}

func orderWriteIsExplicitNull(body json.RawMessage) bool {
	return len(body) > 0 && strings.TrimSpace(string(body)) == "null"
}

func orderWriteUintOf(raw map[string]json.RawMessage, key string) (uint64, bool) {
	body, ok := raw[key]
	if !ok {
		return 0, true
	}
	text := strings.TrimSpace(string(body))
	if text == "" || text[0] < '0' || text[0] > '9' {
		return 0, false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
	}
	var out uint64
	for i := 0; i < len(text); i++ {
		digit := uint64(text[i] - '0')
		if out > (^uint64(0)-digit)/10 {
			return 0, false
		}
		out = out*10 + digit
	}
	return out, true
}

func orderWriteIDsOf(raw map[string]json.RawMessage) ([]string, bool) {
	body, ok := raw["ids"]
	if !ok || orderWriteIsExplicitNull(body) {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, false
	}
	if len(items) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(string(item))
		if text == "" || text == "null" {
			return nil, false
		}
		var value string
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, false
		}
		out = append(out, value)
	}
	return out, true
}

func orderWriteNormalizeIDs(c *gin.Context, raw map[string]json.RawMessage) ([]string, bool) {
	ids, ok := orderWriteIDsOf(raw)
	if !ok {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderenums.ErrInvalidParam)
		return nil, false
	}
	normalized, nerr := bulkids.Normalize(ids)
	if nerr != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, orderBulkTooManyText(nerr))
		return nil, false
	}
	return normalized, true
}

func orderBulkTooManyText(err error) string {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		return bulkids.MsgTooMany
	}
	return fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, tooMany.Max, tooMany.Count)
}

func orderWriteParamText(c *gin.Context) string {
	return orderWriteTextOf(c, orderenums.ErrInvalidParam, "参数不合法")
}

func orderWriteNotFoundText(c *gin.Context) string {
	return orderWriteTextOf(c, orderenums.ErrCouponNotFound, "优惠码不存在")
}

func orderWriteTextOf(c *gin.Context, key, fallback string) string {
	if text := response.TranslateMessage(c, key); text != key {
		return text
	}
	return fallback
}

func orderWriteInternalText(c *gin.Context) string {

	return response.TranslateMessage(c, orderenums.ErrInternal)
}

func orderReasonText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := orderFacingText(err.Error()); msg != "" {
		if text := response.TranslateMessage(c, msg); text != msg {
			return text
		}

		return msg
	}
	logger.Scene(orderWriteScene).
		With("user_id", builtin.GetUserID(c)).
		Error(err, "订单批量写逐条失败")
	return orderWriteInternalText(c)
}
