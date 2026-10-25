package migrations

// register_mail_automation_definition_v2.go — 迁移 621：图定义 v1 → v2 一次性刷平。
//
// ## 为什么这条迁移是 Go 回调而不是 SQL
//
// v1 → v2 的变换内容是「按节点类型决定出口名」：trigger/delay/email/tag 是 next，
// branch（改名 filter）是 yes/no，end 没有。写成 SQL 就是一层套一层的 jsonb_agg +
// CASE + jsonb_object_agg，而**同一张出口名表**在 Go 侧（service/automation_definition.go
// 的 nodeOutlets）已经存在且被校验器与执行器共用 —— 写成两份，下一次加节点类型时
// 必然只改一边。Go 回调直接调那份实现，且能单测。
//
// ## 幂等判据
//
// 判「还有多少行不是 schema 2」：`definition->>'schema'` 缺失（v1 的历史行）或不为 '2'。
// 全部刷完之后计数为 0 → 迁移器跳过；任何一次中途失败都整体回滚（同事务）。
//
// 注意：判据必须是**内容**而不是「表存在」。这条迁移改的是行内容，表一直都在 ——
// 用默认的表存在性检查会让它永远不执行（且看起来像已经跑过）。

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	jsonx "go_wp/pkg/jsonx"
	"go_wp/pkg/logger"
)

// mailDefinitionV2Version 迁移版本号（测试按它定位注册项）。
const mailDefinitionV2Version = "621-mail-automation-definition-v2"

// mailDefinitionV1CountSQL 还有多少行**能升级却还没升**的 v1 定义。
//
// 判据必须是「有没有该做而没做的事」，而不是「有没有 schema<>'2' 的行」：后者把
// 「无法升级的坏行」也算进来，于是那条行永远计入 → 迁移每次启动重跑、每次重打一条
// 同样的 error（重跑本身无害，但「每启动一次就往日志里写一条」是噪音，久了没人看）。
//
// 所以判据与迁移体的处理口径一致：
//   · 升级函数跳过的行 = definition 不是 JSON 对象、或 nodes 不是数组 —— 这类行
//     在下面的 WHERE 里同样被排除（jsonb_typeof），不再计入；
//   · 其余所有 schema<>'2' 的行都会被真的改写，于是刷完后计数为 0。
//
// 末尾的右括号方便嵌进 CASE 表达式（子查询必须包在括号里）。
const mailDefinitionV1CountSQL = `(SELECT COUNT(*) FROM mail_automations
 WHERE definition IS NOT NULL
   AND jsonb_typeof(definition) = 'object'
   AND btrim(coalesce(definition->>'schema', '')) <> '2')`

func init() {
	register(Migration{
		Version:   mailDefinitionV2Version,
		TableName: "mail_automations",
		CheckSQL:  "SELECT CASE WHEN " + mailDefinitionV1CountSQL + " = 0 THEN 1 ELSE 0 END",
		Func:      upgradeMailDefinitionsToV2,
	})
}

// upgradeMailDefinitionsToV2 逐行把 v1 定义刷成 v2。
//
// 逐行读-改-写（不是一条 UPDATE 扫全表）的理由：要复用的是 Go 侧的归一化逻辑，
// 而它按节点处理，表达不成一个 jsonb 表达式。行数规模很小（一个站点的自动化流程
// 通常是几十条），逐行足够。
func upgradeMailDefinitionsToV2(tx *gorm.DB) error {
	type row struct {
		ID         uint64
		Definition []byte
	}
	var rows []row
	// 只取 v1 行：已经在 v2 的行不必重写（也避免把作者正在编辑的行再洗一遍）。
	// 选取口径与 mailDefinitionV1CountSQL 逐字一致 —— 判据与执行体不一致时，「跑完判定
	// 为已完成」与「真的都处理了」会静静分叉（就是上一条迁移注释里说的那种坑）。
	if err := tx.Raw(`SELECT id, definition::text AS definition FROM mail_automations
		WHERE definition IS NOT NULL
		  AND jsonb_typeof(definition) = 'object'
		  AND btrim(coalesce(definition->>'schema', '')) <> '2'`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("读取待升级的流程定义失败: %w", err)
	}
	for _, r := range rows {
		upgraded, err := upgradeOneDefinition(r.Definition)
		if err != nil {
			// 单行坏数据不该让整条迁移失败：图校验发生在保存时，库里可能已经躺着一条坏定义。
			// 跳过它并留一句日志 —— 它会被校验器在启用 / 执行时拒绝，那正是它该有的归宿。
			// 幂等怎么保证：判据（mailDefinitionV1CountSQL）只数**能升级**的行，
			// 这类行本来就不在计数范围内，所以不会因为「跳过了它」而每次启动重跑。
			logger.Scene("init").With("automation_id", r.ID).
				Error(err, "流程定义无法升级到 v2，已跳过（该行仍按 v1 读）")
			continue
		}
		if err := tx.Exec(`UPDATE mail_automations SET definition = ?::jsonb WHERE id = ?`, string(upgraded), r.ID).Error; err != nil {
			return fmt.Errorf("升级流程 %d 的定义失败: %w", r.ID, err)
		}
	}
	return nil
}

// upgradeOneDefinition 单行的 v1 → v2 变换（纯函数，便于单测）。
//
// 这里**不能** import service 包（public/ 不依赖 internal/）：变换本身只有两条规则
// —— 折出边、branch 改名 —— 迁移写完就冻结，不再随代码演进，所以在这里写一份是
// 正确的：迁移要固定「这一次变换」，而 nodeOutlets 会继续演进。
//
// 空对象（`{}`）也要打上 schema:2：它是一份**坏定义**（图校验会拒），但它的**形状**
// 已经是 v2 —— 不打标就会一直被 mailDefinitionV1CountSQL 数到，迁移每次启动重跑。
func upgradeOneDefinition(raw []byte) ([]byte, error) {
	var def map[string]any
	if err := jsonx.Unmarshal(raw, &def); err != nil {
		return nil, err
	}
	if def == nil {
		def = map[string]any{}
	}
	def["schema"] = 2
	nodes, _ := def["nodes"].([]any)
	for i, item := range nodes {
		node, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nodes[i] = upgradeOneNode(node)
	}
	return jsonx.Marshal(def)
}

// upgradeOneNode v1 节点 → v2 节点（迁移期的冻结规则，不跟随 nodeOutlets 演进）。
func upgradeOneNode(node map[string]any) map[string]any {
	typ, _ := node["type"].(string)
	out := map[string]any{}
	if existing, ok := node["out"].(map[string]any); ok {
		for k, v := range existing {
			if s, isStr := v.(string); isStr && strings.TrimSpace(s) != "" {
				out[k] = s
			}
		}
	}
	if len(out) == 0 {
		switch typ {
		case "branch":
			typ = "filter"
			for _, name := range []string{"yes", "no"} {
				if v, ok := node[name].(string); ok && strings.TrimSpace(v) != "" {
					out[name] = v
				}
			}
		case "end":
			// end 没有出口
		default:
			if v, ok := node["next"].(string); ok && strings.TrimSpace(v) != "" {
				out["next"] = v
			}
		}
	}
	for _, k := range []string{"next", "yes", "no"} {
		delete(node, k)
	}
	node["type"] = typ
	node["out"] = out
	if typ == "filter" {
		params, _ := node["params"].(map[string]any)
		if params == nil {
			params = map[string]any{}
			node["params"] = params
		}
		if mode, _ := params["mode"].(string); strings.TrimSpace(mode) == "" {
			params["mode"] = "all"
		}
	}
	return node
}
