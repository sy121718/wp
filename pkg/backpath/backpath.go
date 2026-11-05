package backpath

import (
	"net/url"
	"strings"

	"go_wp/pkg/logger"
	"go_wp/pkg/safepath"
)

const MaxQueryValueBytes = 200

func Build(path string, source url.Values, keys ...string) (string, bool) {
	if !safepath.IsSameOriginPath(path) || len(path) > safepath.MaxReturnPathBytes {
		return "", false
	}
	params := make(map[string]string, len(keys))
	for _, k := range keys {
		v := strings.TrimSpace(source.Get(k))
		if v == "" {
			continue
		}
		if !KeepValue(v) {
			continue
		}
		params[k] = v
	}
	return WithParams(path, params), true
}

func KeepValue(v string) bool {
	if v == "" {
		return false
	}
	return len(v) <= MaxQueryValueBytes && !safepath.HasControlRune(v)
}

func WithParams(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}
	q := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) == "" {
			continue
		}
		q.Set(k, v)
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// FromRequest 按请求查询参数构造安全回跳路径。
//
// 与 Build 同判据（同源、长度、控制字符、取值白名单），
// 但不合法时回退到调用方显式给定的 fallback —— pkg 不写死任何应用路径。
// scene 是日志场景，由调用方决定，框架不替应用命名。
func FromRequest(query url.Values, path, fallback, scene string, keys ...string) string {
	if scene == "" {
		scene = "backpath"
	}
	back, ok := Build(path, query, keys...)
	if !ok {
		logger.Scene(scene).With("path", path).
			Warn("回跳路径不合法（非站内相对路径或超长），回落调用方给定路径")
		return fallback
	}
	for _, k := range keys {
		v := strings.TrimSpace(query.Get(k))
		if v == "" || KeepValue(v) {
			continue
		}
		logger.Scene(scene).With("key", k).With("len", len(v)).
			Warn("回跳参数被丢弃（超长或含控制字符）")
	}
	return back
}
