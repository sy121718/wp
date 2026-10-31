package backpath

import (
	"net/url"
	"strings"

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
