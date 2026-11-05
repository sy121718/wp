package builder

import (
	"strings"

	"golang.org/x/net/html"
)

const fragmentPathPrefix = "/_fragments/"

func fragmentCapsFromHTML(content string) htmlFeatures {
	out := htmlFeatures{}
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			return out
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		_, more := z.TagName()
		for more {
			var key, val []byte
			key, val, more = z.TagAttr()
			if !strings.HasPrefix(strings.ToLower(string(key)), "hx-") {
				continue
			}
			for _, cap := range fragmentCapsIn(string(val)) {
				out[cap] = struct{}{}
			}
		}
	}
}

func fragmentCapsIn(value string) []string {
	var out []string
	rest := value
	for {
		idx := strings.Index(rest, fragmentPathPrefix)
		if idx < 0 {
			return out
		}
		rest = rest[idx+len(fragmentPathPrefix):]
		end := 0
		for end < len(rest) && isFragmentCapByte(rest[end]) {
			end++
		}
		if end == 0 {

			continue
		}
		out = append(out, strings.ToLower(rest[:end]))
		rest = rest[end:]
	}
}

func isFragmentCapByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '_':
		return true
	}
	return false
}

func hasHXAttr(attrs htmlFeatures) bool {
	for name := range attrs {
		if strings.HasPrefix(name, "hx-") {
			return true
		}
	}
	return false
}
