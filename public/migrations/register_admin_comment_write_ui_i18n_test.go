package migrations_test

import (
	"regexp"
	"strings"
	"testing"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

func TestAdminCommentWriteUISeedGuardAgreement(t *testing.T) {
	t.Parallel()
	seed := findSeedByVersion(t, "607-admin-comment-write-ui-i18n")
	want := map[string]string{
		"admin.comment.write.refresh|zh-CN": "刷新列表",
		"admin.comment.write.refresh|en-US": "Refresh list",
		"admin.comment.write.noJs|zh-CN":    "评论审核需要 JavaScript，请开启后重试。",
		"admin.comment.write.noJs|en-US":    "Comment review requires JavaScript. Enable it and try again.",
	}
	pairs := seedPairs(t, seed.SQL)
	if len(pairs) != len(want) {
		t.Fatalf("607 must contain exactly four pairs: %v", pairs)
	}
	seen := map[string]bool{}
	values := commentWriteSeedValues(t, seed.SQL)
	for _, pair := range pairs {
		id := pair.key + "|" + pair.lang
		value, ok := want[id]
		if !ok || seen[id] {
			t.Fatalf("unexpected or duplicate pair %s", id)
		}
		seen[id] = true
		if values[id] != value {
			t.Errorf("%s value = %q, want %q", id, values[id], value)
		}
	}
	const guard = "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
		"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
		"'admin.comment.write.refresh', 'admin.comment.write.noJs')"
	if seed.TableName != "sys_i18n" || seed.ConditionSQL != guard {
		t.Fatalf("guard must cover exactly the four seeded pairs: %s", seed.ConditionSQL)
	}
	if !strings.HasSuffix(strings.TrimSpace(seed.SQL), "ON CONFLICT (item_key, lang) DO NOTHING;") {
		t.Fatal("607 must preserve operator values on conflict")
	}
}

func commentWriteSeedValues(t *testing.T, sqlText string) map[string]string {
	t.Helper()
	rows := regexp.MustCompile(`(?m)^\('([^']+)', '([^']+)', '((?:[^']|'')*)', 200,`).FindAllStringSubmatch(sqlText, -1)
	if len(rows) != 4 {
		t.Fatalf("expected four translation rows, got %d", len(rows))
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row[1]+"|"+row[2]] = strings.ReplaceAll(row[3], "''", "'")
	}
	return values
}

// Production schema comes from the existing isolated migration fixture.
// PREPARE checks parsing and column resolution without executing the seed.
func TestAdminCommentWriteUISeedBackfillsEveryMissingPair(t *testing.T) {
	seed := findSeedByVersion(t, "607-admin-comment-write-ui-i18n")
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE comment_write_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE comment_write_seed").Error
	}); err != nil {
		t.Fatalf("PREPARE seed: %v", err)
	}
	keys := map[string]bool{
		"admin.comment.write.refresh": true,
		"admin.comment.write.noJs":    true,
	}
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key IN (?, ?)",
		"admin.comment.write.refresh", "admin.comment.write.noJs")
	if got := guardValue(t, db, seed); got != 0 {
		t.Fatalf("empty set guard = %d, want 0", got)
	}
	mustExec(t, db, seed.SQL)
	if got := guardValue(t, db, seed); got != 1 {
		t.Fatalf("complete set guard = %d, want 1", got)
	}

	const operatorValue = "Operator edited translation"
	const thirdValue = "Operator third language"
	mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?",
		operatorValue, "admin.comment.write.noJs", "zh-CN")
	mustExec(t, db, "INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) "+
		"VALUES (?, ?, ?, 200, 'admin', 'operator', 1, now(), now())",
		"admin.comment.write.refresh", "ja-JP", thirdValue)

	for _, pair := range seedPairs(t, seed.SQL) {
		t.Run(pair.key+"/"+pair.lang, func(t *testing.T) {
			original := valueOf(t, db, pair.key, pair.lang)
			mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", pair.key, pair.lang)
			// Four total rows remain, but the third language must not hide a missing pair.
			if got := countPairs(t, db, keys, ""); got != 4 {
				t.Fatalf("missing pair plus third language has %d rows, want 4", got)
			}
			if got := guardValue(t, db, seed); got != 0 {
				t.Fatalf("missing %s/%s guard = %d, want 0", pair.key, pair.lang, got)
			}
			for round := 0; round < 2; round++ {
				mustExec(t, db, seed.SQL)
				if got := guardValue(t, db, seed); got != 1 {
					t.Fatalf("after backfill guard = %d, want 1", got)
				}
				if got := valueOf(t, db, pair.key, pair.lang); got != commentWriteSeedValues(t, seed.SQL)[pair.key+"|"+pair.lang] {
					t.Fatalf("backfilled value = %q", got)
				}
				if got := valueOf(t, db, "admin.comment.write.refresh", "ja-JP"); got != thirdValue {
					t.Fatalf("third language changed: %q", got)
				}
				if pair.key != "admin.comment.write.noJs" || pair.lang != "zh-CN" {
					if got := valueOf(t, db, "admin.comment.write.noJs", "zh-CN"); got != operatorValue {
						t.Fatalf("operator value changed: %q", got)
					}
				}
				if got := countPairs(t, db, keys, ""); got != 5 {
					t.Fatalf("after backfill has %d rows, want 5", got)
				}
			}
			// Restore the operator edit when that exact pair was deliberately removed.
			mustExec(t, db, "UPDATE sys_i18n SET item_value = ? WHERE item_key = ? AND lang = ?", original, pair.key, pair.lang)
		})
	}
}
