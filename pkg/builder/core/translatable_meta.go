package core

import (
	"strings"
	"sync"
)

type FieldMeta struct {
	Type string

	Field string

	Kind ControlKind

	MaxLen int
}

func (m FieldMeta) Rich() bool { return m.Kind == ControlRichText }

func TranslatableFieldMeta(typeName, field string) (m FieldMeta, ok bool) {
	typeName = strings.TrimSpace(typeName)
	field = strings.TrimSpace(field)
	if typeName == "" || field == "" {
		return FieldMeta{}, false
	}
	if !TranslatableFields(typeName)[field] {
		return FieldMeta{}, false
	}
	cacheKey := typeName + "." + field
	translatableMetaCacheMu.RLock()
	cached, hit := translatableMetaCache[cacheKey]
	translatableMetaCacheMu.RUnlock()
	if hit {
		return cached, true
	}

	m = FieldMeta{Type: typeName, Field: field}
	if comp, err := Lookup(typeName); err == nil {
		if sp, ok := comp.(SpecProvider); ok {
			if controls, cerr := ParseControls(sp.PropsSpec()); cerr == nil {
				for _, c := range controls {

					if c.Key == field || strings.HasSuffix(c.Key, "."+field) {
						m.Kind, m.MaxLen = c.Kind, c.MaxLen
						break
					}
				}
			}
		}
	}
	translatableMetaCacheMu.Lock()
	translatableMetaCache[cacheKey] = m
	translatableMetaCacheMu.Unlock()
	return m, true
}

var (
	translatableMetaCacheMu sync.RWMutex
	translatableMetaCache   = map[string]FieldMeta{}
)

func resetTranslatableMetaByType(typeName string) {
	prefix := typeName + "."
	translatableMetaCacheMu.Lock()
	for key := range translatableMetaCache {
		if strings.HasPrefix(key, prefix) {
			delete(translatableMetaCache, key)
		}
	}
	translatableMetaCacheMu.Unlock()
}
