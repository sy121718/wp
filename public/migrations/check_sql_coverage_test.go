package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/public/migrations"
)

// 一条迁移的 CheckSQL 是「本迁移已应用」的判定条件：只检查其中一个对象时，
// 存量库只要恰好有那个对象，整条迁移就被跳过，同一文件里其它对象永远不会补建
// —— 实测：wp 库有 idx_pages_blockref，068 被判已应用，structure_header / footer
// 两个索引从未创建。
//
// 但收紧历史迁移的判定同样有害：068 / 111 / 164 / 167 / 171 的 SQL 引用改名前的列
// （created_at / updated_at，201/205 已改成 create_time / update_time），判定一旦
// 变严，终态库与存量库每次启动都会重放它们并必然撞 42703。实测用例
// TestSharedFeedbackArtexRowsUpgrade 就在终态库上重跑整条链。
//
// 因此规则分两段：
//   - 门槛迁移（coverageSince）及之后新增的迁移：必须覆盖本迁移建的全部对象；
//   - 更早的迁移：判定保持原文，只做「不得判终态已删对象」的检查。历史迁移漏掉的
//     对象由新迁移承接（762 / 763 / 764），存量库结构是否齐由 baseline-adopt 的
//     结构与行数核验、schema-snapshot 的终态比对保证，不靠改历史判定兜。
const coverageSince = "761-ai-event-call-log-columns"

var (
	// IF NOT EXISTS 可省：307 这类「先 DROP 再 CREATE」的迁移不写它。
	createObjectRe = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?(?:INDEX|TABLE)\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z0-9_]+)`)
	dropObjectRe   = regexp.MustCompile(`(?i)DROP\s+(?:UNIQUE\s+)?(?:INDEX|TABLE)\s+(?:IF\s+EXISTS\s+)?([a-zA-Z0-9_]+)`)
	indexObjectRe  = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z0-9_]+)`)
	createTableRe  = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?`)
	// 注释里的「CREATE INDEX IF NOT EXISTS。」会让可选组回溯，把关键字 IF 当成对象名。
	lineCommentRe = regexp.MustCompile(`(?m)--[^\n]*`)
)

// sqlKeywords 兜住正则回溯与注释残留：这些词永远不是对象名。
var sqlKeywords = map[string]bool{
	"if": true, "on": true, "not": true, "exists": true, "unique": true,
	"index": true, "table": true, "create": true, "where": true, "using": true,
}

// objectsOf 返回 SQL 里 CREATE 的对象名（剥注释后匹配，关键字过滤）。
func objectsOf(re *regexp.Regexp, sql string) []string {
	clean := lineCommentRe.ReplaceAllString(sql, "")
	var out []string
	for _, m := range re.FindAllStringSubmatch(clean, -1) {
		if sqlKeywords[strings.ToLower(m[1])] {
			continue
		}
		out = append(out, m[1])
	}
	return out
}

func TestMigrationCheckSQLCoversEveryCreatedObject(t *testing.T) {
	all := migrations.All()
	strictFrom := -1
	for i, m := range all {
		if m.Version == coverageSince {
			strictFrom = i
		}
	}
	if strictFrom < 0 {
		t.Fatalf("门槛迁移 %s 不在注册表里；改门槛要同步这条测试", coverageSince)
	}

	for i, m := range all {
		names := objectsOf(createObjectRe, m.SQL)
		if len(names) == 0 {
			continue
		}
		// lastAction 记后续迁移对该对象的最后一次动作；同一迁移内先 DROP 再 CREATE 时
		// CREATE 覆盖 DROP（视为重建），因此 CREATE 循环放在 DROP 之后。
		takenOver := map[string]bool{}
		lastAction := map[string]string{}
		for _, later := range all[i+1:] {
			for _, name := range objectsOf(dropObjectRe, later.SQL) {
				takenOver[name] = true
				lastAction[name] = "drop"
			}
			for _, name := range objectsOf(createObjectRe, later.SQL) {
				takenOver[name] = true
				lastAction[name] = "create"
			}
		}

		mentioned := 0
		for _, name := range names {
			if !strings.Contains(m.CheckSQL, name) {
				continue
			}
			mentioned++
			if lastAction[name] == "drop" {
				t.Errorf("迁移 %s 的 CheckSQL 判了 %s，但后续迁移已把它 DROP 且未重建：终态库永远判不通过，每次启动都会重放本迁移（判定只保留终态仍存在的对象）",
					m.Version, name)
			}
		}

		if i < strictFrom {
			continue
		}

		// 门槛之后的迁移：不判对象名时只有「自己建表」可放行 —— 建表与建索引在同一
		// 事务里，表存在即整条执行过。纯增量迁移（只 ALTER / 只建索引）没有这个保证，
		// 761 就是只判五列，两个索引缺了也整条跳过。
		if mentioned == 0 {
			if createTableRe.MatchString(m.SQL) {
				continue
			}
			var unownedIndexes []string
			for _, idx := range objectsOf(indexObjectRe, m.SQL) {
				if !takenOver[idx] {
					unownedIndexes = append(unownedIndexes, idx)
				}
			}
			if len(unownedIndexes) > 0 {
				t.Errorf("迁移 %s 是纯增量迁移（无 CREATE TABLE）却不判它建的索引：%s；存量库缺索引时会被整条跳过（用 coverObjects 补进 CheckSQL，或由后续迁移接管）",
					m.Version, strings.Join(unownedIndexes, ", "))
			}
			continue
		}

		var missing []string
		for _, name := range names {
			if strings.Contains(m.CheckSQL, name) || takenOver[name] {
				continue
			}
			missing = append(missing, name)
		}
		if len(missing) > 0 {
			t.Errorf("迁移 %s 的 CheckSQL 只覆盖部分对象，缺少 %s；存量库会因判定已应用而漏建（SQL 里的对象：%s）",
				m.Version, strings.Join(missing, ", "), strings.Join(names, ", "))
		}
	}
}
