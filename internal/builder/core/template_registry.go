package core

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ownedTemplatesMu  sync.RWMutex
	ownedTemplatesMap = map[string]string{}
)

func RegisterTemplate(name, source string) {
	if strings.TrimSpace(name) == "" {
		panic("core.RegisterTemplate: 模板名为空")
	}
	if strings.TrimSpace(source) == "" {
		panic(fmt.Sprintf("core.RegisterTemplate: 模板 %q 内容为空", name))
	}
	ownedTemplatesMu.Lock()
	defer ownedTemplatesMu.Unlock()
	ownedTemplatesMap[name] = source
}

func OwnedTemplateNames() []string {
	ownedTemplatesMu.RLock()
	defer ownedTemplatesMu.RUnlock()
	names := make([]string, 0, len(ownedTemplatesMap))
	for n := range ownedTemplatesMap {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func OwnedTemplate(name string) (string, bool) {
	ownedTemplatesMu.RLock()
	defer ownedTemplatesMu.RUnlock()
	s, ok := ownedTemplatesMap[name]
	return s, ok
}
