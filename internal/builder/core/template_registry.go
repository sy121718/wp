package core

// template_registry.go — 组件自带模板的注册表。
//
// 背景：组件模板此前统一放在 internal/templates/components/，与组件自己的 .go 分居两个顶层目录。
// 「一个组件一个目录」要求模板就近：
//
//     internal/builder/components/form/
//       form.go    props + Validate + compileCSS
//       form.css   样式
//       form.jet   模板（本文件让它可以就地 //go:embed 并注册）
//
// 依赖方向：本文件只提供注册与读取，不认识 internal/templates；
// 由装配层（internal/templates 的 loader）主动来取 —— 于是 core 依旧是最底层，不产生环。

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ownedTemplates 组件自带的模板源码（模板名 → 内容）。init 期注册，之后只读。
var (
	ownedTemplatesMu  sync.RWMutex
	ownedTemplatesMap = map[string]string{}
)

// RegisterTemplate 注册组件自带的模板源码（在组件包 init 中调用）。
//
// name 必须与渲染时使用的模板名一致（如 form 组件用 Template: "form"，
// 对应文件 form.jet）。名字或内容为空直接 panic —— 这是 init 期错误，
// 静默注册一个空模板只会让产物少一段 HTML，比启动失败难查得多。
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

// OwnedTemplateNames 返回已注册的模板名（字典序，确定性输出）。
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

// OwnedTemplate 取组件自带模板的源码。
func OwnedTemplate(name string) (string, bool) {
	ownedTemplatesMu.RLock()
	defer ownedTemplatesMu.RUnlock()
	s, ok := ownedTemplatesMap[name]
	return s, ok
}
