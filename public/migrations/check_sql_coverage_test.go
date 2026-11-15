package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/public/migrations"
)

// 一条迁移的 CheckSQL 是「本迁移已应用」的判定条件。它只检查其中一个对象时，
// 存量库只要恰好有那个对象，整条迁移就被跳过，同一文件里其它对象永远不会补建
// —— 实测：wp 库有 idx_pages_blockref，068 被判已应用，structure_header/footer
// 两个索引从未创建（070 之后没有任何迁移补回）。
//
// 规则：CheckSQL 一旦逐对象判定（提到 SQL 里某个 CREATE 对象名），就必须覆盖全部
// CREATE 对象；完全不提对象名的 CheckSQL（判列、判整表条件）不在此列。
var createObjectRe = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?(?:INDEX|TABLE)\s+IF\s+NOT\s+EXISTS\s+([a-zA-Z0-9_]+)`)

// 后续迁移显式删掉的对象不算本迁移的终态：171 建了 idx_build_jobs_running，
// 295-build-jobs-lease 又把它 DROP 掉（回收改按租约到期判定，该索引零命中）。
// 这类对象不要求出现在 CheckSQL 里，否则判定条件会与真实终态相反。
var dropObjectRe = regexp.MustCompile(`(?i)DROP\s+(?:UNIQUE\s+)?(?:INDEX|TABLE)\s+IF\s+EXISTS\s+([a-zA-Z0-9_]+)`)

func TestMigrationCheckSQLCoversEveryCreatedObject(t *testing.T) {
	all := migrations.All()
	for i, m := range all {
		matches := createObjectRe.FindAllStringSubmatch(m.SQL, -1)
		if len(matches) < 2 {
			continue
		}
		droppedLater := map[string]bool{}
		for _, later := range all[i+1:] {
			for _, drop := range dropObjectRe.FindAllStringSubmatch(later.SQL, -1) {
				droppedLater[drop[1]] = true
			}
		}
		names := make([]string, 0, len(matches))
		mentioned := 0
		for _, match := range matches {
			names = append(names, match[1])
			if strings.Contains(m.CheckSQL, match[1]) {
				mentioned++
			}
		}
		if mentioned == 0 {
			continue
		}
		var missing []string
		for _, name := range names {
			if strings.Contains(m.CheckSQL, name) || droppedLater[name] {
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
