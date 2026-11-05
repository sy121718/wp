package core

import (
	"cmp"
	"fmt"
	json "go_wp/pkg/jsonx"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type ControlKind string

const (
	ControlString ControlKind = "string"
	ControlBool   ControlKind = "bool"
	ControlInt    ControlKind = "int"
	ControlSlider ControlKind = "slider"
	ControlSelect ControlKind = "select"
	ControlSafe   ControlKind = "safe"
	ControlText   ControlKind = "text"

	ControlRichText ControlKind = "richtext"
	ControlRegex    ControlKind = "regex"
	ControlURL      ControlKind = "url"

	ControlMedia     ControlKind = "media"
	ControlColor     ControlKind = "color"
	ControlDimension ControlKind = "dimension"
	ControlMargin    ControlKind = "margin"
	ControlSpacing   ControlKind = "spacing"
	ControlCorners   ControlKind = "corners"
	ControlClasses   ControlKind = "classes"
	ControlRText     ControlKind = "rtext"
	ControlNumber    ControlKind = "number"
	ControlCSSDecls  ControlKind = "cssdecls"

	ControlEntityRef ControlKind = "entityref"

	ControlBindingField ControlKind = "bindingfield"

	ControlCollectionField ControlKind = "collectionfield"

	ControlMultiEntityRef ControlKind = "multientityref"

	ControlRangeList ControlKind = "rangelist"
)

const (
	ctTagName     = "ct"
	ctRegexTagKey = "ctRegex"
)

type ControlOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

type Control struct {
	Key     string          `json:"key"`
	Kind    ControlKind     `json:"kind"`
	Label   string          `json:"label,omitempty"`
	Default string          `json:"default,omitempty"`
	Min     int             `json:"min,omitempty"`
	Max     int             `json:"max,omitempty"`
	Step    int             `json:"step,omitempty"`
	MaxLen  int             `json:"maxLen,omitempty"`
	Unit    string          `json:"unit,omitempty"`
	Options []ControlOption `json:"options,omitempty"`

	Prefixes []string `json:"prefixes,omitempty"`
	Pattern  string   `json:"pattern,omitempty"`
	Hidden   bool     `json:"hidden,omitempty"`

	Section string `json:"section,omitempty"`

	goName string
}

func (c Control) SortKey() string { return c.Key }

func ParseControls(props any) (controls []Control, err error) {
	return parseControls(props, "", "")
}

func parseControls(props any, keyPrefix, goPrefix string) (controls []Control, err error) {
	t := reflect.TypeOf(props)

	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("props 必须是结构体或结构体指针")
	}
	for i := range t.NumField() {
		f := t.Field(i)
		tag, ok := f.Tag.Lookup(ctTagName)

		if ok && strings.HasPrefix(strings.TrimSpace(tag), "group") {

			ft := f.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() != reflect.Struct {
				return nil, fmt.Errorf("字段 %s: ct:\"group\" 仅支持结构体或结构体指针", f.Name)
			}
			sub, serr := parseControls(reflect.New(ft).Interface(), keyPrefix+jsonFieldName(f)+".", goPrefix+f.Name+".")
			if serr != nil {
				return nil, serr
			}
			controls = append(controls, sub...)
			continue
		}

		if !ok || strings.TrimSpace(tag) == "" || strings.TrimSpace(tag) == "-" {
			continue
		}
		c, parseErr := parseControlTag(f, tag)
		if parseErr != nil {
			return nil, fmt.Errorf("字段 %s tag 解析失败: %w", f.Name, parseErr)
		}

		if c.Kind == ControlRegex {
			if pat, ok := f.Tag.Lookup(ctRegexTagKey); ok && pat != "" {
				c.Pattern = pat
			} else {
				return nil, fmt.Errorf("字段 %s: regex 控件必须提供 ctRegex tag", f.Name)
			}
		}
		c.Key = keyPrefix + c.Key
		c.goName = goPrefix + c.goName
		controls = append(controls, c)
	}
	return controls, nil
}

func jsonFieldName(f reflect.StructField) string {
	if j := strings.Split(f.Tag.Get("json"), ",")[0]; j != "" && j != "-" {
		return j
	}
	return f.Name
}

func fieldByPath(v reflect.Value, path string) (reflect.Value, bool) {
	for _, part := range strings.Split(path, ".") {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		v = v.FieldByName(part)
		if !v.IsValid() {
			return reflect.Value{}, false
		}
	}
	return v, true
}

func parseControlTag(f reflect.StructField, tag string) (c Control, err error) {
	parts := strings.Split(tag, ",")
	if len(parts) == 0 || parts[0] == "" {
		return c, fmt.Errorf("缺少控件类型")
	}
	c.goName = f.Name
	c.Key = f.Name
	if j := strings.Split(f.Tag.Get("json"), ",")[0]; j != "" && j != "-" {
		c.Key = j
	}
	c.Kind = ControlKind(strings.TrimSpace(parts[0]))
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case part == "hidden":
			c.Hidden = true
		case strings.HasPrefix(part, "label="):
			c.Label = strings.TrimPrefix(part, "label=")
		case strings.HasPrefix(part, "default="):
			c.Default = strings.TrimPrefix(part, "default=")
		case strings.HasPrefix(part, "min="):
			if c.Min, err = strconv.Atoi(strings.TrimPrefix(part, "min=")); err != nil {
				return c, fmt.Errorf("min 非法: %s", part)
			}
		case strings.HasPrefix(part, "max="):
			if c.Max, err = strconv.Atoi(strings.TrimPrefix(part, "max=")); err != nil {
				return c, fmt.Errorf("max 非法: %s", part)
			}
		case strings.HasPrefix(part, "maxlen="):
			if c.MaxLen, err = strconv.Atoi(strings.TrimPrefix(part, "maxlen=")); err != nil {
				return c, fmt.Errorf("maxlen 非法: %s", part)
			}
		case strings.HasPrefix(part, "step="):
			if c.Step, err = strconv.Atoi(strings.TrimPrefix(part, "step=")); err != nil || c.Step < 1 {
				return c, fmt.Errorf("step 非法: %s", part)
			}
		case strings.HasPrefix(part, "unit="):
			c.Unit = strings.TrimPrefix(part, "unit=")
		case strings.HasPrefix(part, "sec="):
			c.Section = strings.TrimPrefix(part, "sec=")
		case strings.HasPrefix(part, "prefixes="):

			for _, p := range strings.Split(strings.TrimPrefix(part, "prefixes="), "|") {
				if p = strings.TrimSpace(p); p != "" {
					c.Prefixes = append(c.Prefixes, p)
				}
			}
		default:

			if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
				c.Options = append(c.Options, ControlOption{Value: kv[0], Label: kv[1]})
			} else {
				c.Options = append(c.Options, ControlOption{Value: part})
			}
		}
	}
	if c.Section == "" {
		c.Section = "content"
	}
	if c.Kind == ControlSelect && len(c.Options) == 0 {
		return c, fmt.Errorf("select 控件必须提供选项")
	}
	if c.Kind == ControlEntityRef && len(c.Options) == 0 {
		return c, fmt.Errorf("entityref 控件必须指定实体类型（如 category）")
	}
	if c.Kind == ControlMultiEntityRef && len(c.Options) == 0 {
		return c, fmt.Errorf("multientityref 控件必须指定实体类型（如 tag）")
	}

	if len(c.Prefixes) > 0 && c.Kind != ControlBindingField && c.Kind != ControlCollectionField {
		return c, fmt.Errorf("prefixes 只适用于 bindingfield / collectionfield 控件（当前 %s）", c.Kind)
	}
	return c, nil
}

func ValidateSpec(props any, nodeID string) (err error) {
	controls, err := ParseControls(props)
	if err != nil {
		return fmt.Errorf("节点 %s: %w", nodeID, err)
	}
	v := reflect.ValueOf(props)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	for _, c := range controls {
		fv, reachable := fieldByPath(v, c.goName)
		if !reachable {

			continue
		}
		if err = validateControlValue(c, fv, nodeID); err != nil {
			return err
		}
	}
	return nil
}

func validateControlValue(c Control, fv reflect.Value, nodeID string) (err error) {
	msg := func(detail string, args ...any) error {
		return fmt.Errorf("节点 %s: 字段 %s %s", nodeID, c.Key, fmt.Sprintf(detail, args...))
	}

	switch fv.Kind() {
	case reflect.String:
		s := fv.String()
		if s == "" {
			return nil
		}
		if c.MaxLen > 0 && len(s) > c.MaxLen {
			return msg("超长（上限 %d 字符）", c.MaxLen)
		}
		switch c.Kind {
		case ControlSelect:
			found := false
			values := make([]string, 0, len(c.Options))
			for _, opt := range c.Options {
				values = append(values, opt.Value)
				if opt.Value == s {
					found = true
					break
				}
			}
			if !found {
				return msg("值 %q 不在选项内（有效值: %s）", s, strings.Join(values, "/"))
			}
		case ControlMedia:

			if strings.ContainsAny(s, " \t\"'<>`;") {
				return msg("媒体值非法: %q", s)
			}
		case "boxspacing":

			if !IsSafeCSSValue(s) {
				return msg("值非法: %q", s)
			}
		case ControlCSSDecls:
			if !IsSafeCSSDecls(s) {
				return msg("CSS 声明非法: %q", s)
			}
		case ControlSafe, ControlColor, ControlDimension, ControlMargin:
			if !IsSafeCSSValue(s) {
				return msg("值非法: %q", s)
			}
		case ControlRegex:
			if c.Pattern != "" && !regexp.MustCompile(c.Pattern).MatchString(s) {
				return msg("值不匹配模式: %q", s)
			}
		case ControlURL:
			if !IsSafeURL(s) {
				return msg("链接协议非法: %q", s)
			}
		case ControlEntityRef:
			if _, parseErr := uuid.Parse(s); parseErr != nil {
				return msg("实体 id 非法: %q", s)
			}
		}
	case reflect.Bool:

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		n := fv.Int()
		if n == 0 {
			return nil
		}
		if c.Min != 0 && n < int64(c.Min) {
			return msg("小于下限 %d: %d", c.Min, n)
		}
		if c.Max != 0 && n > int64(c.Max) {
			return msg("超出上限 %d: %d", c.Max, n)
		}
	case reflect.Float32, reflect.Float64:

		f := fv.Float()
		if f == 0 {
			return nil
		}
		if c.Min != 0 && f < float64(c.Min) {
			return msg("小于下限 %d: %v", c.Min, f)
		}
		if c.Max != 0 && f > float64(c.Max) {
			return msg("超出上限 %d: %v", c.Max, f)
		}
	}
	return nil
}

var urlSafeRe = regexp.MustCompile(`^[A-Za-z0-9./:?=&%~#+_@-]{1,500}$`)

func IsSafeURL(s string) bool {
	if !urlSafeRe.MatchString(s) {
		return false
	}
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "/") {
		return true
	}
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "mailto:") ||
		strings.HasPrefix(s, "tel:")
}

var sectionOrder = map[string]int{"content": 0, "style": 1, "border": 2, "hover": 3, "motion": 4, "advanced": 5}

func SchemaJSON(props any) (data []byte, err error) {
	controls, err := ParseControls(props)
	if err != nil {
		return nil, err
	}
	type schemaItem struct {
		Key     string          `json:"key"`
		Kind    string          `json:"kind"`
		Label   string          `json:"label,omitempty"`
		Section string          `json:"section,omitempty"`
		Default string          `json:"default,omitempty"`
		Min     int             `json:"min,omitempty"`
		Max     int             `json:"max,omitempty"`
		Step    int             `json:"step,omitempty"`
		MaxLen  int             `json:"maxLen,omitempty"`
		Unit    string          `json:"unit,omitempty"`
		Options []ControlOption `json:"options,omitempty"`
		Hidden  bool            `json:"hidden,omitempty"`
	}

	buckets := map[string][]schemaItem{}
	for _, c := range controls {
		sec := c.Section
		if sec == "" {
			sec = "content"
		}
		item := schemaItem{
			Key: c.Key, Kind: string(c.Kind), Label: c.Label, Section: sec, Default: c.Default,
			Min: c.Min, Max: c.Max, Step: c.Step, MaxLen: c.MaxLen, Unit: c.Unit, Options: c.Options, Hidden: c.Hidden,
		}
		buckets[sec] = append(buckets[sec], item)
	}
	var items []schemaItem
	secs := make([]string, 0, len(buckets))
	for sec := range buckets {
		secs = append(secs, sec)
	}
	rank := func(sec string) int {
		if v, ok := sectionOrder[sec]; ok {
			return v
		}
		return len(sectionOrder) + 1
	}
	slices.SortFunc(secs, func(a, b string) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), strings.Compare(a, b))
	})
	for _, sec := range secs {
		items = append(items, buckets[sec]...)
	}
	return json.Marshal(items)
}
