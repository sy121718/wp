package core

// translatable.go — 组件可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）。
//
// 定位：内容翻译（sys_translation）只处理**作者在编辑器里填写的文本**（按钮文字、
// 标题、alt、图注、富文本），与 P4 的组件固定文案（sys_i18n key）是两套东西，
// 同一页面共存、互不覆盖（决策 F17）。
//
// 铁律（决策 F6）：
//   - 只有组件显式声明在白名单里的字段才参与翻译；
//   - 未声明字段**永不翻译** —— 防止 /shop、#FF0000、1200、variant 被当成文本翻掉；
//   - 白名单是唯一可翻译性来源，构建期不猜、不按「看起来像中文」判断。
//
// 声明方式（组件作者）：
//
//	Atom 基座组件：core.AtomSpec[Props]{TypeName: Type, Translatable: []string{"text"}}
//	自定义结构组件：func (c *Component) Translatable() []string { return []string{"title"} }
//
// 注册期校验（§7.5 规则 2）：字段名必须存在于组件 Props 的 JSON 字段集合，拼错即
// 注册失败（init 期 panic，fail-fast），绝不静默放过。

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TranslatableProvider 由「含作者填写文本」的组件实现：返回参与内容翻译的字段名
// 白名单（JSON 字段名，如 text / alt / caption）。
//
// 返回 nil / 空切片表示该组件没有任何可翻译字段（结构型容器、纯样式组件）。
type TranslatableProvider interface {
	Translatable() []string
}

// translatableFieldRe 白名单字段名格式：JSON 字段名（字母开头，字母/数字/下划线）。
// 显式排除 . [ ] —— context 是「类型.字段名」，字段名自带点号会产生歧义（§7.5 规则 4）。
var translatableFieldRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// ValidateTranslatable 校验组件声明的可翻译字段白名单。
//
// spec 为组件 Props 零值指针（SpecProvider.PropsSpec()，可为 nil）；
// spec 非 nil 时字段必须存在于 Props 的 JSON 字段集合（递归，含嵌套结构体与切片元素）。
func ValidateTranslatable(spec any, fields []string) (err error) {
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if !translatableFieldRe.MatchString(f) {
			return fmt.Errorf("字段名 %q 非法（仅允许字母/数字/下划线，不含 . 与 [ ]）", f)
		}
		if seen[f] {
			return fmt.Errorf("字段名 %q 重复声明", f)
		}
		seen[f] = true
	}
	if spec == nil || len(fields) == 0 {
		return nil
	}
	names := jsonFieldNames(spec)
	for _, f := range fields {
		if !names[f] {
			return fmt.Errorf("字段 %q 不在组件 Props 中（白名单拼错即拒绝，docs/06-D §7.5 规则 2）", f)
		}
	}
	return nil
}

// ContentContextFor 拼装实体字段的内容译文语境 "{实体类型}.{字段名}"（docs/06-D §7.5）。
//
// 与 pkg/i18n.ContentContext 同一拼法：本函数供领域模块（商品域的分类/品牌/标签/属性）
// 在**不依赖 pkg/i18n 语境工具**的前提下拼语境，保证全站语境只有一处语义
// （任一段为空返回空串，空语境不参与取词，取词器直接回退原文）。
func ContentContextFor(entityType, field string) string {
	typ := strings.TrimSpace(entityType)
	f := strings.TrimSpace(field)
	if typ == "" || f == "" {
		return ""
	}
	return typ + "." + f
}

// jsonFieldNames 递归收集结构体的 JSON 字段名集合。
//
// 覆盖：本层字段、嵌套结构体字段（如 items[].title 的 title）、指针/切片/数组/映射
// 的元素结构体字段。跳过 json:"-" 与不可导出字段；time.Time 不下钻。
func jsonFieldNames(spec any) map[string]bool {
	out := map[string]bool{}
	collectJSONFieldNames(reflect.TypeOf(spec), out, map[reflect.Type]bool{})
	return out
}

// collectJSONFieldNames 递归实现（seen 按类型去重，防自引用类型无限递归）。
func collectJSONFieldNames(t reflect.Type, out map[string]bool, seen map[reflect.Type]bool) {
	for t != nil && (t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice ||
		t.Kind() == reflect.Array || t.Kind() == reflect.Map) {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct || seen[t] || t == reflect.TypeOf(time.Time{}) {
		return
	}
	seen[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := translatableJSONFieldName(f)
		if name == "" {
			continue
		}
		out[name] = true
		collectJSONFieldNames(f.Type, out, seen)
	}
}

// translatableJSONFieldName 取结构体字段的 JSON 名（无 tag 用字段名；json:"-" 返回空串）。
//
// 与 controls.go 的 jsonFieldName 区别：这里把 json:"-" 视为「不参与」（返回空串），
// 而控件 schema 侧退回 Go 字段名。
func translatableJSONFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	if i := strings.Index(tag, ","); i >= 0 {
		tag = tag[:i]
	}
	if tag == "-" {
		return ""
	}
	if tag != "" {
		return tag
	}
	return f.Name
}

// TranslatableFields 返回组件声明的可翻译字段集合（未声明 / 未注册返回 nil）。
//
// 构建期收集与替换共用本函数，保证「声明即唯一来源」；结果按类型缓存
// （组件注册表在 init 后不变，白名单随组件编译期静态声明）。
func TranslatableFields(typeName string) map[string]bool {
	translatableCacheMu.RLock()
	cached, ok := translatableCache[typeName]
	translatableCacheMu.RUnlock()
	if ok {
		return cached
	}
	comp, err := Lookup(typeName)
	if err != nil {
		return nil
	}
	tp, ok := comp.(TranslatableProvider)
	if !ok {
		return nil
	}
	fields := tp.Translatable()
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		out[f] = true
	}
	translatableCacheMu.Lock()
	translatableCache[typeName] = out
	translatableCacheMu.Unlock()
	return out
}

// translatableCache 类型 → 白名单集合（见 TranslatableFields）。
// Register 会清空缓存，便于测试替换组件。
//
// 并发约束：同一进程内多个构建 worker 会并发查询本缓存（cache miss 冷启动路径），
// 裸 map 并发写会触发 fatal error: concurrent map writes——该错误不可 recover，
// 会直接终止整个服务进程。所有读写一律经 translatableCacheMu。
var (
	translatableCacheMu sync.RWMutex
	translatableCache   = map[string]map[string]bool{}
)

// resetTranslatableFields 失效单个类型的白名单缓存（组件注册表变化时调用）。
func resetTranslatableFields(typeName string) {
	translatableCacheMu.Lock()
	delete(translatableCache, typeName)
	translatableCacheMu.Unlock()
}
