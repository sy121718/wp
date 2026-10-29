package core

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"go_wp/pkg/upload"
	xhtml "golang.org/x/net/html"
)

// NormalizeRichMediaURLs 仅归一现有富文本白名单中的 img.src；不扩展标签或属性能力。
// 本地历史地址必须明确属于 storage，外链保留原域名，不能按访问请求的 Host 猜地址。
func NormalizeRichMediaURLs(src string) (string, error) {
	if !strings.Contains(strings.ToLower(src), "<img") {
		return src, nil
	}
	z := xhtml.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	index := 0
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			return out.String(), nil
		}
		raw := string(z.Raw())
		if tt != xhtml.StartTagToken && tt != xhtml.SelfClosingTagToken {
			out.WriteString(raw)
			continue
		}
		tok := z.Token()
		if tok.Data != "img" {
			out.WriteString(raw)
			continue
		}
		index++
		changed := false
		for i := range tok.Attr {
			attr := &tok.Attr[i]
			if attr.Key != "src" {
				continue
			}
			if !isSafeRichHref(attr.Val) && strings.Contains(stripRichHrefNoise(attr.Val), ":") {
				// 危险协议继续交由同一白名单清洗器剥离，不能被归类为可容错的缺内容。
				continue
			}
			value := strings.TrimSpace(attr.Val)
			parsed, err := url.Parse(value)
			if err != nil {
				return "", fmt.Errorf("富文本图片 %d 的 src 不是有效媒体地址", index)
			}
			if !parsed.IsAbs() && parsed.Host == "" {
				if !strings.HasPrefix(parsed.Path, "/storage/") && !strings.HasPrefix(parsed.Path, "storage/") {
					return "", fmt.Errorf("%w: 富文本图片 %d 的 src 是无法确定归属的相对地址，请选择媒体或填写完整 HTTP(S) 地址", ErrIncompleteNode, index)
				}
				if upload.StorageKey(value) == "" {
					return "", fmt.Errorf("富文本图片 %d 的 src 不是有效本地媒体地址", index)
				}
				if upload.BaseURL() == "" {
					return "", fmt.Errorf("%w: 富文本图片 %d 的 src 缺少可信媒体基址", ErrIncompleteNode, index)
				}
			}
			resolved := upload.StorageURL(value)
			if resolved == "" {
				return "", fmt.Errorf("富文本图片 %d 的 src 必须使用完整 HTTP(S) 媒体地址", index)
			}
			if attr.Val != resolved {
				attr.Val, changed = resolved, true
			}
		}
		if changed {
			out.WriteString(tok.String())
		} else {
			out.WriteString(raw)
		}
	}
}

// NormalizeRichMediaProps 在安全校验完成后的渲染入口处理声明字段的副本。
// 不能放入 DecodeProps：配置未完成也必须先通过全部属性与 Advanced 安全校验。
func NormalizeRichMediaProps(props any) error {
	return normalizeRichMediaValue(reflect.ValueOf(props), "")
}

func normalizeRichMediaValue(v reflect.Value, parent string) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return normalizeRichMediaValue(v.Elem(), parent)
	}
	if v.Kind() == reflect.Array || v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			if err := normalizeRichMediaValue(v.Index(i), fmt.Sprintf("%s[%d]", parent, i)); err != nil {
				return err
			}
		}
		return nil
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	// core.text 的纯文本模式只显示字面文本，不将其中的 HTML 示例解释成图片。
	plain := v.FieldByName("Mode")
	isPlain := plain.IsValid() && plain.Kind() == reflect.String && plain.String() == "plaintext"
	for i := 0; i < v.NumField(); i++ {
		field, spec := v.Field(i), v.Type().Field(i)
		if !field.CanSet() {
			continue
		}
		name := strings.Split(spec.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = spec.Name
		}
		path := name
		if parent != "" {
			path = parent + "." + name
		}
		if strings.Split(spec.Tag.Get("ct"), ",")[0] == "richtext" && field.Kind() == reflect.String {
			if isPlain {
				continue
			}
			normalized, err := NormalizeRichMediaURLs(field.String())
			if err != nil {
				return fmt.Errorf("富文本字段 %s: %w", path, err)
			}
			field.SetString(normalized)
		} else if err := normalizeRichMediaValue(field, path); err != nil {
			return err
		}
	}
	return nil
}
