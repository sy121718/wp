package mailservice

// automation_definition.go — 图定义 v2：出口名表 / 归一化 / 出边访问器。
//
// ## 为什么要有这一层
//
// 定义从 v1（节点上扁平 `next` / `yes` / `no` 三个字段）升级到 v2
// （统一 `out map[string]string`），原因是节点类型要扩到 10 种，而出边数量与含义
// 各不相同：`filter` 是 yes/no，`ab_test` 是 2–5 个变体 id，`end` 没有。用「每加一种
// 就多一个结构体字段」的写法，`AutomationNode` 会变成一个只有编译器看得懂的联合体，
// 而执行器里 20 多处 `node.Yes` / `node.No` 的分支会随着类型增加不断分叉。
//
// `out` 把「出边叫什么」变成数据：新增节点类型只改本文件的 `nodeOutlets` 表，
// 校验（nodeArity）、执行器（outlet 访问器）、画布 meta、模板结构树都读同一张表。
//
// ## v1 与 v2 绝不共存于同一个节点的出边表达
//
// `normalizeDefinition` 读完立刻产出 v2，之后全链路只见 `out`。v1 只在两个入口存在：
// 迁移前落库的历史行，以及还没升级的外部调用方提交的请求体。
//
// ## 未知字段 / 未知出口一律报错，不默认 pass
//
// 这与执行器里 `evalConditions` 的既定语义一致：配置写错必须伪装不成「条件不满足」，
// 否则表现是「这批人怎么都走了 no 分支」，事后极难查。

import (
	"sort"
	"strings"

	mailenums "go_wp/internal/module/mail/enums"
)

// automationSchemaV2 当前图定义的 schema 版本。
const automationSchemaV2 = 2

// automationSchemaV1 v1 的隐式版本号（v1 的定义里没有 schema 字段）。
const automationSchemaV1 = 1

// 出口名（`out` 的键）。只有这里出现的名字才被认可 —— 校验与执行器都引用这些常量，
// 字符串字面量不许散在业务代码里（写错一个字母的表现是「边静默消失」）。
const (
	outletNext        = "next"
	outletYes         = "yes"
	outletNo          = "no"
	outletNotInList   = "not_in_list"
	outletActive      = "active"
	outletNonActive   = "non_active"
	outletABVariantID = "v" // 变体 id 的形态：v1…v5
)

// automationV1OnlyTypes v1 里存在、v2 里已并入其它类型的节点类型。
//
// `branch` 改名成 `filter` 是**读取时**的语义升级：两者的出边都是 yes/no，条件都在
// `params.conditions` 里，唯一的差别是 v2 多了一个 `mode`（all / any）。保留两个类型名
// 会让「条件节点」有两套形状，此后每次改条件都要记得改两处。
var automationV1OnlyTypes = map[string]string{
	NodeTypeBranch: NodeTypeFilter,
}

// nodeOutlets 返回该类型的**有序**出口名。
//
// `ab_test` 返回空切片：它的出口是动态的变体 id，名字来自 `params.variants`，
// 不能写死在这里（`nodeOutlets` 只回答「有没有固定出口」，不回答「这一份定义的
// 出口是哪几个」—— 后者要读 params，见 `nodeArity`）。
func nodeOutlets(nodeType string) []string {
	if mapped, ok := automationV1OnlyTypes[nodeType]; ok {
		nodeType = mapped
	}
	switch nodeType {
	case NodeTypeTrigger, NodeTypeDelay, NodeTypeEmail, NodeTypeTag, NodeTypeWebhook:
		return []string{outletNext}
	case NodeTypeFilter:
		return []string{outletYes, outletNo}
	case NodeTypeListStatusBranch:
		return []string{outletNotInList, outletActive, outletNonActive}
	case NodeTypeEnd:
		return []string{}
	default:
		return nil
	}
}

// normalizeDefinition 把任意版本的图定义升级到 v2，返回（v2 定义, 目标 schema 版本）。
//
// 三条硬约束：
//  1. **深拷贝**入参：调用方传进来的 map 可能被别处持有（请求体、库里的 JSONMap），
//     归一化过程中改它会让「只读一次」变成「悄悄改了别人的数据」；
//  2. **只做形状升级，不做语义校验**：校验在 ParseDefinition → ValidateDefinition，
//     归一化只负责让 v1 与 v2 变成同一种形状（重复 key / 未知 type 都原样透传，
//     由校验器给出可定位的错误）；
//  3. 已经有的 `out` 不被扁平字段覆盖：v2 定义里残留的 `next` 是历史垃圾，忽略它。
func normalizeDefinition(raw map[string]any) (map[string]any, int) {
	out, _ := normalizeDefinitionVersioned(raw)
	return out, automationSchemaV2
}

// normalizeDefinitionVersioned 与 normalizeDefinition 相同，但同时返回**输入**的 schema 版本。
//
// 迁移（621）需要知道「这一行原本是 v1 还是 v2」才能只改 v1；单独的
// normalizeDefinition 只回答目标版本，因此没有把版本折进返回值 —— 那会让
// 每个调用点多一个用不上的返回值。
func normalizeDefinitionVersioned(raw map[string]any) (map[string]any, int) {
	if len(raw) == 0 {
		return map[string]any{}, automationSchemaV1
	}
	source := schemaOf(raw)
	dst := make(map[string]any, len(raw)+1)
	for k, v := range raw {
		dst[k] = deepCopyJSON(v)
	}
	dst["schema"] = automationSchemaV2
	// 入口 key 与顶层其它字段原样保留（只做类型规整：JSON 解出来的数字是 float64，
	// 但 entry 只可能是字符串，写回字符串形态让下游断言稳定）。
	if entry, ok := raw["entry"].(string); ok {
		dst["entry"] = entry
	}
	nodes, ok := raw["nodes"].([]any)
	if !ok {
		return dst, source
	}
	normalized := make([]any, 0, len(nodes))
	for _, item := range nodes {
		node, isMap := item.(map[string]any)
		if !isMap {
			// 非对象的节点原样透传：让 ValidateDefinition 报「没有 key 的节点」，
			// 而不是在这里丢掉它（丢了会变成「节点数对不上」这种更模糊的症状）。
			normalized = append(normalized, deepCopyJSON(item))
			continue
		}
		normalized = append(normalized, normalizeNode(node))
	}
	dst["nodes"] = normalized
	return dst, source
}

// schemaOf 读定义里的 schema 字段（缺省 = v1）。
func schemaOf(raw map[string]any) int {
	switch v := raw["schema"].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return automationSchemaV1
}

// normalizeNode 把一个节点升级到 v2 形状。
//
// v2 节点（已经有 `out`）只做类型改名，出边一个字不动；v1 节点按类型的固定出口
// 从扁平字段折进 `out`。空出边**不入 map**：`{"next": ""}` 与「没有 next」在下游
// 是同一种意思，进去只会让 `outgoing()` 多一次空串判断。
func normalizeNode(node map[string]any) map[string]any {
	typ, _ := node["type"].(string)
	renamed := typ
	if v2Type, ok := automationV1OnlyTypes[typ]; ok {
		renamed = v2Type
	}
	out := make(map[string]any, 3)
	if existing, ok := node["out"].(map[string]any); ok {
		for k, v := range existing {
			if s, isStr := v.(string); isStr && strings.TrimSpace(s) != "" {
				out[k] = s
			}
		}
	}
	if len(out) == 0 {
		for _, name := range nodeOutlets(typ) {
			if v, ok := node[name].(string); ok && strings.TrimSpace(v) != "" {
				out[name] = v
			}
		}
	}
	dst := make(map[string]any, len(node)+3)
	for k, v := range node {
		switch k {
		case "next", "yes", "no", "out", "type":
			continue // 下面单独处理
		default:
			dst[k] = deepCopyJSON(v)
		}
	}
	dst["type"] = renamed
	dst["out"] = out
	// v1 的 branch 没有 mode：补 all（与 v1「全部满足才走 yes」的 AND 语义一致）。
	if renamed == NodeTypeFilter {
		params, _ := dst["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
			dst["params"] = params
		}
		if mode, _ := params["mode"].(string); strings.TrimSpace(mode) == "" {
			params["mode"] = ConditionModeAll
		}
	}
	return dst
}

// deepCopyJSON 深拷贝 JSON 形状的值（map / slice 逐层复制，标量直接返回）。
//
// 为什么不用序列化往返：归一化在**每次读取定义**时都跑（列表页会读一批），
// 往返一次对一个几百字节的图不算慢，但它是纯开销；而这里的形状是封闭的
// （JSON 解出来的只有 nil / bool / float64 / string / []any / map[string]any）。
func deepCopyJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		dst := make(map[string]any, len(t))
		for k, item := range t {
			dst[k] = deepCopyJSON(item)
		}
		return dst
	case []any:
		dst := make([]any, len(t))
		for i, item := range t {
			dst[i] = deepCopyJSON(item)
		}
		return dst
	default:
		return v
	}
}

// outlet 读该节点某个出口指向的目标。
//
// 弃用的扁平字段在这里兜底（v1 定义若绕过 normalizeDefinition 直接落到结构体上，
// 仍然读得到）：入口只有 ParseDefinition 一个，这条兜底是给**单测直接构造结构体**
// 与「迁移还没跑」的库行留的后路，不是给业务代码的第二条路径。
func (n AutomationNode) outlet(name string) string {
	if n.Out != nil {
		if v, ok := n.Out[name]; ok {
			return v
		}
	}
	switch name {
	case outletNext:
		return n.Next
	case outletYes:
		return n.Yes
	case outletNo:
		return n.No
	}
	return ""
}

// allOutlets 返回该节点所有出边（按出口名排序，便于日志与单测稳定）。
//
// 只返回**非空**的出边：空的出口在下游是同一种意思（没有这一条）。
func (n AutomationNode) allOutlets() map[string]string {
	seen := make(map[string]string, len(n.Out)+3)
	add := func(name, target string) {
		if strings.TrimSpace(target) != "" {
			seen[name] = target
		}
	}
	if len(n.Out) > 0 {
		for name, target := range n.Out {
			add(name, target)
		}
	} else {
		// v2 里没有 out 的节点只可能是直接构造的结构体（见 outlet 的说明）。
		add(outletNext, n.Next)
		add(outletYes, n.Yes)
		add(outletNo, n.No)
	}
	return seen
}

// outletNames 该节点实际填了的出口名（有序，供校验报错与测试断言）。
func (n AutomationNode) outletNames() []string {
	filled := n.allOutlets()
	names := make([]string, 0, len(filled))
	for name := range filled {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// displayOutlets 一个节点在界面上会画出的出口名（固定出口 + 动态变体 id）。
//
// 预览与校验共用：`ab_test` 的出口来自它自己的变体定义，其余读 nodeOutlets。
func displayOutlets(n AutomationNode) []string {
	if n.Type == NodeTypeABTest {
		return abVariantIDs(n.Params)
	}
	return nodeOutlets(n.Type)
}

// abVariantIDs 从 `params.variants` 里取出变体 id（保序）。
func abVariantIDs(params map[string]any) []string {
	if params == nil {
		return nil
	}
	list, ok := params["variants"].([]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		v, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		if id, isStr := v["id"].(string); isStr && strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	return ids
}

// unknownOutlets 返回 `out` 里出现的、该类型不认识的出口名（有序）。
//
// 未知出口必须报错：拼错一个出口名（`"yse"`）在导出场景里表现为「这条边静默消失」，
// 而图校验的其余判据（悬空边 / 可达性）都不会命中它。
func unknownOutlets(n AutomationNode) []string {
	allowed := make(map[string]bool, 6)
	for _, name := range displayOutlets(n) {
		allowed[name] = true
	}
	var unknown []string
	for name := range n.allOutlets() {
		if !allowed[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// outletDisplayName 出口名在界面与错误里的说法（取词交给调用点的 enums）。
func outletDisplayName(name string) string {
	if name == "" {
		return "(空)"
	}
	return name
}

// graphErrUnknownOutlet 造一条「未知出口名」的图校验错误（供 nodeArity 使用）。
func graphErrUnknownOutlet(key, name string) error {
	return graphErr(mailenums.DetailGraphUnknownOutlet,
		"节点 {node}: 未知出口名: {outlet}", "node", key, "outlet", outletDisplayName(name))
}
