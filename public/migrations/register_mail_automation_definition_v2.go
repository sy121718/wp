package migrations

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	jsonx "go_wp/pkg/jsonx"
	"go_wp/pkg/logger"
)

const mailDefinitionV2Version = "621-mail-automation-definition-v2"

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

func upgradeMailDefinitionsToV2(tx *gorm.DB) error {
	type row struct {
		ID         uint64
		Definition []byte
	}
	var rows []row

	if err := tx.Raw(`SELECT id, definition::text AS definition FROM mail_automations
		WHERE definition IS NOT NULL
		  AND jsonb_typeof(definition) = 'object'
		  AND btrim(coalesce(definition->>'schema', '')) <> '2'`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("读取待升级的流程定义失败: %w", err)
	}
	for _, r := range rows {
		upgraded, err := upgradeOneDefinition(r.Definition)
		if err != nil {

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
