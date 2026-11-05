package i18n

import (
	"strings"
)

const ErrDetailSep = "\x1f"

const ErrDetailPartSep = "\x1e"

type ErrorDetailPart struct {
	Key  string
	Args map[string]string
}

func ErrorDetail(key string, kv ...string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if strings.ContainsAny(kv[i+1], ErrDetailSep+ErrDetailPartSep) {
			return ""
		}
	}
	var b strings.Builder
	b.WriteString(ErrDetailSep)
	b.WriteString(key)
	for i := 0; i+1 < len(kv); i += 2 {
		name := strings.TrimSpace(kv[i])
		if name == "" || strings.ContainsAny(name, ErrDetailSep+"=") {
			continue
		}
		b.WriteString(ErrDetailSep)
		b.WriteString(name)
		b.WriteString("=")
		b.WriteString(kv[i+1])
	}
	return b.String()
}

func JoinErrorDetails(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, ErrDetailPartSep)
}

func ParseErrorDetails(raw string) (parts []ErrorDetailPart, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, ErrDetailSep) {
		return nil, false
	}
	for _, chunk := range strings.Split(raw, ErrDetailPartSep) {
		part, partOK := parseErrorDetailPart(chunk)
		if !partOK {
			return nil, false
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

func parseErrorDetailPart(raw string) (part ErrorDetailPart, ok bool) {
	segs := strings.Split(raw, ErrDetailSep)

	if len(segs) < 2 {
		return ErrorDetailPart{}, false
	}
	key := strings.TrimSpace(segs[1])
	if key == "" {
		return ErrorDetailPart{}, false
	}
	args := make(map[string]string, len(segs)-2)
	for _, seg := range segs[2:] {
		if seg == "" {
			continue
		}
		name, value, found := strings.Cut(seg, "=")
		if !found || strings.TrimSpace(name) == "" {
			return ErrorDetailPart{}, false
		}
		args[name] = value
	}
	return ErrorDetailPart{Key: key, Args: args}, true
}

func ParseErrorDetail(raw string) (key string, args map[string]string, ok bool) {
	parts, partsOK := ParseErrorDetails(raw)
	if !partsOK {
		return "", nil, false
	}
	return parts[0].Key, parts[0].Args, true
}

// EncodedDetailLike 判断一段文本是不是编码明细的残留（含段分隔符或反斜杠转义形态）：
// 这类串不能直接交给前端显示。
func EncodedDetailLike(raw string) bool {
	if strings.ContainsAny(raw, ErrDetailSep+ErrDetailPartSep) {
		return true
	}
	return strings.Contains(raw, `\x1f`) || strings.Contains(raw, `\x1e`)
}

var detailLogReplacer = strings.NewReplacer(ErrDetailSep, "|", ErrDetailPartSep, "||")

func DetailTailForLog(raw string) string {
	out := detailLogReplacer.Replace(strings.TrimSpace(raw))
	const maxRunes = 200
	if r := []rune(out); len(r) > maxRunes {
		out = string(r[:maxRunes]) + "…"
	}
	return out
}
