package mailservice

// mail_automation_graph.go — 流程图的定义与校验（issue #38 P3）。
//
// 图校验是引擎的地基：一条指向不存在节点的边、一个死循环、一个永远走不到的节点，
// 在运行期表现为「某个人莫名其妙卡住了」，很难查。所以在**保存时**就拒绝。
//
// 五条校验：
//   1. 必须有 entry 且指向存在的节点；
//   2. 节点 key 非空且唯一；
//   3. 类型的出边形状正确（email / delay / tag 是单出边，branch 是两条，end 没有）；
//   4. 所有出边指向存在的节点；
//   5. **无环**，且从 entry 可达所有节点。
//
// 第 5 条为什么必须有：有环的图在运行期就是无限循环 —— 每一轮都会给同一个人发一次邮件，
// 直到把发信额度烧完或被收件方投诉。可达性是另一半：不可达的节点是作者写错了，
// 静默留着会让人以为「配好了」。

import (
	"encoding/json"
	"strings"

	mailenums "go_wp/internal/module/mail/enums"
)

// 节点类型。
const (
	NodeTypeTrigger = "trigger"
	NodeTypeDelay   = "delay"
	NodeTypeEmail   = "email"
	NodeTypeBranch  = "branch"
	NodeTypeTag     = "tag"
	NodeTypeEnd     = "end"
)

// AutomationDefinition 图定义。
type AutomationDefinition struct {
	Entry string           `json:"entry"`
	Nodes []AutomationNode `json:"nodes"`
}

// AutomationNode 一个节点。
type AutomationNode struct {
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
	// Next 单出边（trigger / delay / email / tag 用）。
	Next string `json:"next,omitempty"`
	// Yes / No 条件分支的两条出边（branch 用）。
	Yes string `json:"yes,omitempty"`
	No  string `json:"no,omitempty"`

	// X / Y 节点在可视化画布上的位置（P4）。
	//
	// **引擎完全忽略它们** —— 它们是编辑器的布局数据，不是流程语义。放同一个 JSONB 里
	// 是因为「节点的位置」与「节点本身」同生命周期（删节点即删位置），拆出去反而要维护两份。
	// 保存位置走单独的接口、**不推进版本号**：挪一下位置不该让正在跑的实例「版本落后」。
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

// outgoing 返回该节点的所有出边。
func (n AutomationNode) outgoing() []string {
	switch n.Type {
	case NodeTypeBranch:
		out := make([]string, 0, 2)
		if n.Yes != "" {
			out = append(out, n.Yes)
		}
		if n.No != "" {
			out = append(out, n.No)
		}
		return out
	case NodeTypeEnd:
		return nil
	default:
		if n.Next != "" {
			return []string{n.Next}
		}
		return nil
	}
}

// ParseDefinition 解析并校验图定义；返回纯 Go 结构供执行器使用。
func ParseDefinition(raw map[string]any) (*AutomationDefinition, error) {
	if len(raw) == 0 {
		return nil, graphErr(mailenums.DetailGraphEmptyDefinition, "流程定义不能为空")
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var def AutomationDefinition
	if err := json.Unmarshal(b, &def); err != nil {
		return nil, graphErr(mailenums.DetailGraphNotGraph,
			"流程定义不是合法的图结构: {reason}", "reason", err.Error())
	}
	if err := ValidateDefinition(&def); err != nil {
		return nil, err
	}
	return &def, nil
}

// ValidateDefinition 校验图定义，返回第一条可定位的问题。
func ValidateDefinition(def *AutomationDefinition) error {
	if def == nil || len(def.Nodes) == 0 {
		return graphErr(mailenums.DetailGraphNoNode, "流程里至少要有一个节点")
	}

	byKey := make(map[string]AutomationNode, len(def.Nodes))
	order := make([]string, 0, len(def.Nodes))
	for _, n := range def.Nodes {
		key := strings.TrimSpace(n.Key)
		if key == "" {
			return graphErr(mailenums.DetailGraphNodeKeyMissing, "存在没有 key 的节点")
		}
		if _, dup := byKey[key]; dup {
			return graphErr(mailenums.DetailGraphNodeKeyDup, "节点 key 重复: {node}", "node", key)
		}
		if _, err := nodeArity(n); err != nil {
			// 明细自带 {node} 参数（nodeArity 拿得到节点自身），这里不再包一层「节点 X:」——
			// 包了参数会重复出现两次，译文读起来是「节点 n1: 节点 n1: 等待…」。
			return err
		}
		byKey[key] = n
		order = append(order, key)
	}

	entry := strings.TrimSpace(def.Entry)
	if entry == "" {
		return graphErr(mailenums.DetailGraphEntryMissing, "没有指定入口节点")
	}
	if _, ok := byKey[entry]; !ok {
		return graphErr(mailenums.DetailGraphEntryNotExist, "入口节点不存在: {node}", "node", entry)
	}

	// 出边必须指向存在的节点。
	for _, n := range def.Nodes {
		for _, to := range n.outgoing() {
			if _, ok := byKey[to]; !ok {
				return graphErr(mailenums.DetailGraphEdgeTargetMissing,
					"节点 {from} 指向了不存在的节点 {to}", "from", n.Key, "to", to)
			}
		}
	}

	// 环检测 + 可达性：一次 DFS 同时得到两个结论。
	const (
		white = 0 // 未访问
		gray  = 1 // 在当前路径上（撞到即为环）
		black = 2 // 已完成
	)
	color := make(map[string]int, len(byKey))
	var visit func(key string, path []string) error
	visit = func(key string, path []string) error {
		switch color[key] {
		case gray:
			return graphErr(mailenums.DetailGraphCycle,
				"流程里有环: {path}", "path", strings.Join(append(path, key), " → "))
		case black:
			return nil
		}
		color[key] = gray
		n := byKey[key]
		for _, to := range n.outgoing() {
			if err := visit(to, append(path, key)); err != nil {
				return err
			}
		}
		color[key] = black
		return nil
	}
	if err := visit(entry, nil); err != nil {
		return err
	}

	// 不可达节点：作者写错了，静默留着会让人以为「配好了」。
	var unreachable []string
	for _, key := range order {
		if color[key] == white {
			unreachable = append(unreachable, key)
		}
	}
	if len(unreachable) > 0 {
		return graphErr(mailenums.DetailGraphUnreachable,
			"有节点从入口走不到: {nodes}", "nodes", strings.Join(unreachable, ", "))
	}
	return nil
}

// nodeArity 校验节点类型的出边形状与必需参数。
//
// 明细自带 {node} 定位参数（与本函数返回的中文原文同源）：图校验错误要按当前语言
// 展示给作者，参数化的定位比「有一条边配错了」有用得多。调用方（ValidateDefinition）
// 因此不再包一层「节点 X:」。
func nodeArity(n AutomationNode) (int, error) {
	key := strings.TrimSpace(n.Key)
	switch n.Type {
	case NodeTypeTrigger:
		// 入口节点：有出边就往前走，没有就是「触发即结束」（合法但少用）。
		return 1, nil
	case NodeTypeDelay:
		mins, _ := toInt(n.Params["minutes"])
		if mins <= 0 {
			return 0, graphErr(mailenums.DetailGraphNeedMinutes,
				"节点 {node}: 等待节点需要正数的 minutes", "node", key)
		}
		return 1, nil
	case NodeTypeEmail:
		if strings.TrimSpace(toString(n.Params["template_key"])) == "" {
			return 0, graphErr(mailenums.DetailGraphNeedTemplate,
				"节点 {node}: 发信节点需要 template_key", "node", key)
		}
		return 1, nil
	case NodeTypeBranch:
		if n.Yes == "" || n.No == "" {
			return 0, graphErr(mailenums.DetailGraphNeedTwoArms,
				"节点 {node}: 条件分支需要 yes 与 no 两条出边", "node", key)
		}
		if len(toStringSlice(n.Params["conditions"])) == 0 {
			return 0, graphErr(mailenums.DetailGraphNeedCondition,
				"节点 {node}: 条件分支需要至少一个条件", "node", key)
		}
		return 2, nil
	case NodeTypeTag:
		add := toStringSlice(n.Params["add"])
		remove := toStringSlice(n.Params["remove"])
		if len(add) == 0 && len(remove) == 0 {
			return 0, graphErr(mailenums.DetailGraphNeedTagAction,
				"节点 {node}: 标签节点需要 add 或 remove", "node", key)
		}
		return 1, nil
	case NodeTypeEnd:
		return 0, nil
	default:
		return 0, graphErr(mailenums.DetailGraphUnknownNodeTyp,
			"节点 {node}: 未知节点类型: {type}", "node", key, "type", n.Type)
	}
}

// ---- 小工具（JSONB 解出来的数字是 float64，取整要小心）----

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toStringSlice(v any) []string {
	switch arr := v.(type) {
	case []string:
		return arr
	case []any:
		out := make([]string, 0, len(arr))
		for _, it := range arr {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
