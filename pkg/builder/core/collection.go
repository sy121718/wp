package core

import (
	"fmt"
	"go_wp/pkg/builder/source"
	"strconv"
	"strings"
)

type CollectionResolver = source.CollectionResolver

type CollectionSchema = source.CollectionSchema

type CollectionSchemaProvider = source.CollectionSchemaProvider

type CollectionQuery = source.CollectionQuery
type CollectionPage = source.CollectionPage
type CollectionPager = source.CollectionPager

const ItemFieldPrefix = "item."

type ItemScope struct {
	Inner ContentResolver
	Item  map[string]any
}

func (s ItemScope) ResolveString(field string) (string, error) {
	if name, ok := strings.CutPrefix(field, ItemFieldPrefix); ok {
		return ItemFieldText(s.Item, name), nil
	}
	if s.Inner == nil {
		return "", nil
	}
	return s.Inner.ResolveString(field)
}

func ItemFieldText(item map[string]any, field string) string {
	if field == "" || item == nil {
		return ""
	}
	v, ok := item[field]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if len(t) == 0 {
			return ""
		}
		return ItemFieldText(map[string]any{"v": t[0]}, "v")
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "是"
		}
		return "否"
	default:
		return fmt.Sprint(t)
	}
}

type CollectionSource struct {
	Name string `json:"name"`

	Source string `json:"source"`

	Fields []string `json:"fields"`

	Filters []CollectionFilter `json:"filters,omitempty"`

	OrderBy []string `json:"orderBy,omitempty"`
}

type CollectionFilter = source.CollectionFilter
