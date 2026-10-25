package core

// props_legacy.go — 组件 props 的**旧字段名兼容层**（改名不迁移数据）。
//
// ## 为什么需要它
//
// 组件 props 改过名（`buttonText` / `btnText` / `linkText` → `label`）。改名本身是对的
// ——同一个概念在三个组件里叫三个名字，读代码要重新猜。但改名时有两条路：
//
//	改 tag + 迁移存量数据      写迁移把库里 6 张表、5.7 万条快照里的键改写
//	改 tag + 保留旧名归一      反序列化时把旧键的值挪到新字段
//
// 本项目对**已发布内容**有硬约束：改名不迁移数据（`AGENTS.md` 不变量 3 的同一个精神；
// 动效命名也一样处理 —— 旧入场名留在白名单里产出同一份关键帧）。所以走第二条。
//
// ## 不做归一会长什么样（实测过）
//
//	旧文档 props: {"buttonText":"了解更多","buttonLink":"/about"}
//	反序列化后:   Label="" ButtonLink="/about"
//	BuildView:    HasButton = Label != "" && ButtonLink != ""  →  false
//	产物:         整颗 <a> 不渲染 —— 按钮**静默消失**，不报错、不 500
//
// 影响面（本仓实测）：blueprints 2 张、content_templates 1 张、
// document_snapshots 5.78 万条含旧键；97 个已发布实例走商品详情页模板。
//
// ## 实现选择：为什么在反序列化层而不是 Validate / BuildView
//
// 归一放在唯一入口（`json.Unmarshal`）之后，下游**全部不必知道旧名存在** ——
// BuildView、CompileCSS、TranslatableFields、检查器 schema 都只认新字段。
// 若放在 BuildView 里，那么 CSS 编译（它读 Props）与翻译白名单还得各归一一次，
// 漏一处就是「样式对但文案不翻译」这种更难查的形态。

import (
	"fmt"
	json "go_wp/pkg/jsonx"
)

// LegacyKey 一条旧字段名映射：旧 JSON 键 → 目标字段的现有 JSON 键。
type LegacyKey struct {
	// From 旧文档里的键名（已经不再写进任何 struct tag）。
	From string
	// To 现有字段的 JSON 键名（写入时不产生这个键，只用于把值挪过去）。
	To string
}

// ApplyLegacyKeys 把 raw 里的旧键值挪到新键名下，返回**改写后的 raw**。
//
// 语义（三条都要精确，任一含糊都会让存量文档处理错）：
//
//  1. **新键已存在且非空 → 旧键忽略**。这是"作者已经用新编辑器改过这一屏"的形态，
//     以新值优先（旧值只是历史残留）。
//  2. **新键不存在或为空串 → 用旧值**，并从 raw 里删掉旧键（不再往下游传）。
//  3. **旧键本身不存在 → 原样返回 raw（零分配）**。绝大多数节点没有旧键，
//     这条决定了它不会给构建带来可感知开销。
//
// 空串也算"没有值"：改名前的检查器允许清空按钮文案，那种文档存的是 `"buttonText": ""`
// ——它与键不存在等价，都该让位给旧值判断，而不是把空串当成"作者设了空值"。
func ApplyLegacyKeys(raw json.RawMessage, legacy []LegacyKey) (json.RawMessage, error) {
	if len(raw) == 0 || len(legacy) == 0 {
		return raw, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		// 解析失败不吞：调用方（decodeProps）本来就要报"props 反序列化失败"，
		// 这里再包一层能把节点上下文留在错误链上。
		return raw, fmt.Errorf("旧字段名归一失败: %w", err)
	}

	changed := false
	for _, lk := range legacy {
		oldVal, hasOld := obj[lk.From]
		if !hasOld {
			continue
		}
		curVal, hasCur := obj[lk.To]
		// 规则 1：新键有非空值 → 旧键丢弃（新值优先）。
		if hasCur && !isJSONEmptyString(curVal) {
			delete(obj, lk.From)
			changed = true
			continue
		}
		// 规则 2：新键无值 → 旧值接管。
		obj[lk.To] = oldVal
		delete(obj, lk.From)
		changed = true
	}
	if !changed {
		return raw, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw, fmt.Errorf("旧字段名归一后序列化失败: %w", err)
	}
	return out, nil
}

// isJSONEmptyString 判断一个 JSON 值是否为 `""`（只认字符串空值，null 不算）。
func isJSONEmptyString(v json.RawMessage) bool {
	return string(v) == `""`
}

// NormalizeLegacy 一个 Props 类型实现它即获得「旧字段名自动归一」。
//
// 用法（组件侧，三行）：
//
//	func (p *Props) NormalizeLegacy(raw json.RawMessage) error {
//		return core.ApplyLegacyKeysInto(raw, p, cardLegacyKeys)
//	}
//
// 为什么是**方法而不是接口返回值**：Props 的反序列化散在 10+ 个调用点
// （jetview 的 decodeProps、richdoc 的导出、pluginview 的探针、容器的子节点…），
// 逐个加归一必然漏。让组件自己声明「我有旧名」，再由 `DecodeProps` 统一调用，
// 新入口只要走 DecodeProps 就自动获得兼容。
type LegacyNormalizer interface {
	// NormalizeLegacy 把 raw 里的旧键值挪进接收者已解码的字段上。
	NormalizeLegacy(raw json.RawMessage) error
}

// ApplyLegacyKeysInto 把旧键值归一到 dst（dst 必须已由调用方完成基础解码）。
//
// 与 ApplyLegacyKeys 的区别：那个返回改写后的 raw（供再来一次 Unmarshal），
// 这个直接把值写进已解码的结构体 —— 少一次序列化往返，适合放在 UnmarshalJSON 钩子里。
func ApplyLegacyKeysInto(raw json.RawMessage, dst any, legacy []LegacyKey) error {
	if len(raw) == 0 || len(legacy) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("旧字段名归一失败: %w", err)
	}
	patched := make(map[string]json.RawMessage, len(legacy))
	for _, lk := range legacy {
		oldVal, hasOld := obj[lk.From]
		if !hasOld {
			continue
		}
		curVal, hasCur := obj[lk.To]
		if hasCur && !isJSONEmptyString(curVal) {
			continue // 新值优先
		}
		patched[lk.To] = oldVal
	}
	if len(patched) == 0 {
		return nil
	}
	// 只把「补出来的新键」再解一遍到 dst 上：已有字段保持原解码结果，
	// 不必担心把整个结构体重解一次（那样会覆盖掉调用方已经赋的值）。
	patch, err := json.Marshal(patched)
	if err != nil {
		return fmt.Errorf("旧字段名归一后序列化失败: %w", err)
	}
	if err := json.Unmarshal(patch, dst); err != nil {
		return fmt.Errorf("旧字段名归一后反序列化失败: %w", err)
	}
	return nil
}

// DecodeProps 组件 props 的统一解码入口：先解基础字段，再让组件归一到自己的字段上。
//
// 所有反序列化入口都应当走这里（而不是裸 json.Unmarshal），否则旧名归一就会被跳过 ——
// 而跳过的表现是「某个入口里按钮消失、另一个入口里正常」这种最难查的形态。
func DecodeProps[P any](raw json.RawMessage) (P, error) {
	var p P
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	if n, ok := any(&p).(LegacyNormalizer); ok {
		if err := n.NormalizeLegacy(raw); err != nil {
			return p, err
		}
	}
	return p, nil
}
