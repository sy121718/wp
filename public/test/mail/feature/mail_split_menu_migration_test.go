package feature

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	migrations "go_wp/public/migrations"
	"go_wp/public/test/support"
)

const (
	mailSplitSeedVersion = "521-mail-marketing-menu-split"

	mailSplitDirTitle = "营销"

	mailSplitLegacyPath = "/admin/mail/marketing"
)

type mailSplitMenuRow struct {
	ID             int64
	Title          string
	Path           string
	ParentID       int64
	Type           int
	SortOrder      int
	IsHidden       int
	IsSystem       int
	PermissionCode string
}

func mailSplitSeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, s := range migrations.AllSeeds() {
		if s.Version == mailSplitSeedVersion {
			return s
		}
	}
	t.Fatalf("种子 %s 未注册到 migrations.AllSeeds()", mailSplitSeedVersion)
	return migrations.Seed{}
}

func mailSplitDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}

	var regclass *string
	if err := db.Raw("SELECT to_regclass('sys_menus')::text").Scan(&regclass).Error; err != nil {
		t.Fatalf("检查 sys_menus 是否存在失败: %v", err)
	}
	if regclass == nil || *regclass == "" {
		t.Fatal("测试库缺少 sys_menus 表：生产迁移未生效")
	}
	return db
}

func mailSplitApplySeeds(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行全部种子失败: %v", err)
	}
}

func mailSplitReplay521(t *testing.T, db *gorm.DB) {
	t.Helper()
	seed := mailSplitSeed(t)
	stmts := migrations.SplitStatements(seed.SQL)
	if len(stmts) == 0 {
		t.Fatal("521 的 SQL 拆分后为空：注册内容异常")
	}
	for _, stmt := range stmts {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放 521 语句失败（幂等性不成立）: %v\nSQL: %s", err, stmt)
		}
	}
}

func mailSplitDecision(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var got int64
	if err := db.Raw(mailSplitSeed(t).ConditionSQL).Scan(&got).Error; err != nil {
		t.Fatalf("执行 521 判定 SQL 失败: %v", err)
	}
	return got
}

func mailSplitMenuByPath(t *testing.T, db *gorm.DB, path string) (mailSplitMenuRow, bool) {
	t.Helper()
	var row mailSplitMenuRow
	err := db.Raw(`SELECT id, title, path, coalesce(parent_id, 0) AS parent_id, type,
			sort_order, is_hidden, is_system, coalesce(permission_code, '') AS permission_code
		FROM sys_menus WHERE path = ? AND deleted_at IS NULL ORDER BY id LIMIT 1`, path).Scan(&row).Error
	if err != nil {
		t.Fatalf("查询菜单 %s 失败: %v", path, err)
	}
	return row, row.ID != 0
}

func mailSplitTopDir(t *testing.T, db *gorm.DB, title string) (mailSplitMenuRow, bool) {
	t.Helper()
	var row mailSplitMenuRow
	err := db.Raw(`SELECT id, title, path, coalesce(parent_id, 0) AS parent_id, type,
			sort_order, is_hidden, is_system, coalesce(permission_code, '') AS permission_code
		FROM sys_menus WHERE coalesce(parent_id, 0) = 0 AND type = 1 AND title = ? AND deleted_at IS NULL
		ORDER BY id DESC LIMIT 1`, title).Scan(&row).Error
	if err != nil {
		t.Fatalf("查询顶级目录 %s 失败: %v", title, err)
	}
	return row, row.ID != 0
}

func mailSplitCount(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_menus WHERE path = ? AND deleted_at IS NULL", path).Scan(&n).Error; err != nil {
		t.Fatalf("统计菜单 %s 行数失败: %v", path, err)
	}
	return n
}

func mailSplitSnapshot(t *testing.T, db *gorm.DB) []mailSplitMenuRow {
	t.Helper()
	var rows []mailSplitMenuRow
	if err := db.Raw(`SELECT id, title, path, coalesce(parent_id, 0) AS parent_id, type,
			sort_order, is_hidden, is_system, coalesce(permission_code, '') AS permission_code
		FROM sys_menus WHERE deleted_at IS NULL ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatalf("读取 sys_menus 快照失败: %v", err)
	}
	return rows
}

func TestMailSplitMenuMigrationAppliesExpectedStructure(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	dir, ok := mailSplitTopDir(t, db, mailSplitDirTitle)
	if !ok {
		t.Fatal("521 之后应存在一级目录「营销」，实际没有")
	}
	if dir.SortOrder != 5 {
		t.Errorf("「营销」的 sort_order 应为 5（站点 / 系统顺延一位后空出的位置），实际 %d", dir.SortOrder)
	}
	if dir.Path != "" || dir.IsSystem != 1 {
		t.Errorf("「营销」应为系统内置目录（path 为空、is_system=1），实际 path=%q is_system=%d", dir.Path, dir.IsSystem)
	}

	want := []struct {
		path      string
		title     string
		sortOrder int
		code      string
	}{
		{"/admin/mail/contacts", "联系人", 1, "mail:contact_list"},
		{"/admin/mail/campaigns", "群发活动", 2, "mail:campaign_list"},
		{"/admin/mail/templates", "邮件模板", 3, "mail:template_list"},
		{"/admin/mail/automation", "自动化任务", 4, "mail:automation_list"},
	}
	for _, w := range want {
		row, found := mailSplitMenuByPath(t, db, w.path)
		if !found {
			t.Errorf("521 之后「营销」目录下应存在 %s，实际没有", w.path)
			continue
		}
		if row.ParentID != dir.ID {
			t.Errorf("%s 应挂在「营销」(%d) 下，实际 parent_id=%d", w.path, dir.ID, row.ParentID)
		}
		if row.Title != w.title {
			t.Errorf("%s 的标题应为 %q，实际 %q", w.path, w.title, row.Title)
		}
		if row.SortOrder != w.sortOrder {
			t.Errorf("%s 的 sort_order 应为 %d，实际 %d", w.path, w.sortOrder, row.SortOrder)
		}
		if row.PermissionCode != w.code {
			t.Errorf("%s 的 permission_code 应为 %q（改码会让已授权角色静默缩权），实际 %q",
				w.path, w.code, row.PermissionCode)
		}
		if n := mailSplitCount(t, db, w.path); n != 1 {
			t.Errorf("%s 应只有 1 行菜单，实际 %d 行", w.path, n)
		}
	}

	if _, found := mailSplitMenuByPath(t, db, mailSplitLegacyPath); found {
		t.Errorf("521 之后旧路径 %s 的菜单行应已消失（该行被改造为 /admin/mail/campaigns）", mailSplitLegacyPath)
	}

	reportBefore, found := mailSplitMenuByPath(t, db, "/admin/mail/campaign")
	if !found {
		t.Fatal("迁移不该删除 /admin/mail/campaign 行（隐藏而非删行，否则角色静默缩权）")
	}
	mailSplitReplay521(t, db)
	reportAfter, _ := mailSplitMenuByPath(t, db, "/admin/mail/campaign")
	if reportBefore.IsHidden != 1 {
		t.Errorf("521 之后 /admin/mail/campaign 应被隐藏（is_hidden=1，否则它会出现在侧栏），实际 %d",
			reportBefore.IsHidden)
	}
	if reportAfter.IsHidden != 1 {
		t.Errorf("重放后 /admin/mail/campaign 应仍为隐藏，实际 is_hidden=%d", reportAfter.IsHidden)
	}
	if reportAfter.Path != "/admin/mail/campaign" {
		t.Errorf("521 不该改动 /admin/mail/campaign 的路径，实际 %q", reportAfter.Path)
	}
	if reportAfter.ParentID != reportBefore.ParentID {
		t.Errorf("521 不该改动 /admin/mail/campaign 的挂载点：重放前 %d、重放后 %d",
			reportBefore.ParentID, reportAfter.ParentID)
	}

	account, found := mailSplitMenuByPath(t, db, "/admin/mail")
	if !found {
		t.Fatal("521 不该动 /admin/mail 行")
	}
	system, ok := mailSplitTopDir(t, db, "系统")
	if !ok {
		t.Fatal("应存在一级目录「系统」")
	}
	if account.ParentID != system.ID {
		t.Errorf("/admin/mail 应留在「系统」(%d) 下，实际 parent_id=%d", system.ID, account.ParentID)
	}

	if got := mailSplitDecision(t, db); got != 1 {
		t.Errorf("完整落地态下 521 判定应返回 1，实际 %d", got)
	}
}

func TestMailSplitMenuMigrationIsIdempotent(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	before := mailSplitSnapshot(t, db)
	if len(before) == 0 {
		t.Fatal("种子执行后 sys_menus 为空：前置不成立")
	}

	mailSplitApplySeeds(t, db)
	if got := mailSplitSnapshot(t, db); !reflect.DeepEqual(before, got) {
		t.Errorf("再次执行种子后 sys_menus 发生变化（判定跳过失效）:\n before=%+v\n  after=%+v", before, got)
	}

	mailSplitReplay521(t, db)
	after := mailSplitSnapshot(t, db)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("重放 521 后 sys_menus 发生变化（SQL 不幂等）:\n before=%+v\n  after=%+v", before, after)
	}

	for _, path := range []string{"/admin/mail/contacts", "/admin/mail/templates", "/admin/mail/campaigns"} {
		if n := mailSplitCount(t, db, path); n != 1 {
			t.Errorf("重放后 %s 应仍只有 1 行，实际 %d 行（重复插行）", path, n)
		}
	}
	if _, found := mailSplitMenuByPath(t, db, mailSplitLegacyPath); found {
		t.Errorf("重放后旧路径 %s 不该复活", mailSplitLegacyPath)
	}
	if got := mailSplitDecision(t, db); got != 1 {
		t.Errorf("重放后 521 判定应仍为 1，实际 %d", got)
	}

	dir, _ := mailSplitTopDir(t, db, mailSplitDirTitle)
	var shifted []mailSplitMenuRow
	for _, title := range []string{"站点", "系统"} {
		row, ok := mailSplitTopDir(t, db, title)
		if !ok {
			t.Fatalf("应存在一级目录 %q", title)
		}
		if row.SortOrder <= dir.SortOrder {
			t.Errorf("「%s」应排在「营销」(sort=%d) 之后，实际 sort=%d", title, dir.SortOrder, row.SortOrder)
		}
		shifted = append(shifted, row)
	}
	mailSplitReplay521(t, db)
	for _, want := range shifted {
		got, _ := mailSplitTopDir(t, db, want.Title)
		if got.SortOrder != want.SortOrder {
			t.Errorf("「%s」的 sort_order 被反复顺延：重放前 %d、重放后 %d",
				want.Title, want.SortOrder, got.SortOrder)
		}
	}

	hideStmt := ""
	for _, stmt := range migrations.SplitStatements(mailSplitSeed(t).SQL) {
		if strings.Contains(stmt, "/admin/mail/campaign") && strings.Contains(stmt, "is_hidden = 1") {
			hideStmt = stmt
			break
		}
	}
	if hideStmt == "" {
		t.Fatal("521 的 SQL 里找不到隐藏报表行的第 7 段（被删了？判定门槛会静默失效）")
	}
	res := db.Exec(hideStmt)
	if res.Error != nil {
		t.Fatalf("重放隐藏报表行的语句失败: %v", res.Error)
	}
	if res.RowsAffected != 0 {
		t.Errorf("隐藏报表行的语句重放时影响了 %d 行，应为 0 行（条件里必须带 is_hidden = 0）", res.RowsAffected)
	}
	if got := mailSplitSnapshot(t, db); !reflect.DeepEqual(before, got) {
		t.Errorf("重放隐藏语句后 sys_menus 发生变化（第 7 段不幂等）:\n before=%+v\n  after=%+v", before, got)
	}
}

func TestMailSplitMenuConditionRejectsPartialStates(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	if got := mailSplitDecision(t, db); got != 1 {
		t.Fatalf("前置：完整落地态下判定应返回 1，实际 %d（判定本身不可满足，后续反例无意义）", got)
	}

	if err := db.Exec("DELETE FROM sys_menus WHERE path = ?", "/admin/mail/contacts").Error; err != nil {
		t.Fatalf("构造缺联系人态失败: %v", err)
	}
	if got := mailSplitDecision(t, db); got != 0 {
		t.Errorf("缺 /admin/mail/contacts 时判定必须返回 0，实际 %d", got)
	}

	mailSplitReplay521(t, db)
	if err := db.Exec("DELETE FROM sys_menus WHERE path = ?", "/admin/mail/templates").Error; err != nil {
		t.Fatalf("构造缺邮件模板态失败: %v", err)
	}
	if got := mailSplitDecision(t, db); got != 0 {
		t.Errorf("缺 /admin/mail/templates 时判定必须返回 0，实际 %d", got)
	}

	mailSplitReplay521(t, db)
	if err := db.Exec("UPDATE sys_menus SET path = ? WHERE path = ?", mailSplitLegacyPath, "/admin/mail/campaigns").Error; err != nil {
		t.Fatalf("构造缺群发活动态失败: %v", err)
	}
	if got := mailSplitDecision(t, db); got != 0 {
		t.Errorf("缺 /admin/mail/campaigns（旧行仍在）时判定必须返回 0，实际 %d", got)
	}

	mailSplitReplay521(t, db)
	if err := db.Exec(`INSERT INTO sys_menus (permission_code, title, parent_id, type, path, icon, status,
			is_hidden, is_public, is_system, sort_order, create_time, update_time)
		VALUES ('mail:campaign_list', '邮件营销', 0, 2, ?, 'mail', 1, 0, 0, 1, 9, now(), now())`,
		mailSplitLegacyPath).Error; err != nil {
		t.Fatalf("构造旧路径复活态失败: %v", err)
	}
	if got := mailSplitDecision(t, db); got != 0 {
		t.Errorf("旧路径 %s 复活时判定必须返回 0，实际 %d", mailSplitLegacyPath, got)
	}

	if err := db.Exec("DELETE FROM sys_menus WHERE path = ?", mailSplitLegacyPath).Error; err != nil {
		t.Fatalf("清理旧路径探针行失败: %v", err)
	}
	mailSplitReplay521(t, db)
	if got := mailSplitDecision(t, db); got != 1 {
		t.Errorf("清理后判定应回到 1，实际 %d", got)
	}

	if err := db.Exec("UPDATE sys_menus SET is_hidden = 0 WHERE path = ?", "/admin/mail/campaign").Error; err != nil {
		t.Fatalf("构造报表行可见态失败: %v", err)
	}
	if got := mailSplitDecision(t, db); got != 0 {
		t.Errorf("/admin/mail/campaign 仍可见（is_hidden=0）时判定必须返回 0，实际 %d", got)
	}

	mailSplitReplay521(t, db)
	row, found := mailSplitMenuByPath(t, db, "/admin/mail/campaign")
	if !found || row.IsHidden != 1 {
		t.Fatalf("重放后报表行应存在且已隐藏，实际 found=%v is_hidden=%d", found, row.IsHidden)
	}
	if got := mailSplitDecision(t, db); got != 1 {
		t.Errorf("重放隐藏报表行后判定应回到 1，实际 %d", got)
	}

	if err := db.Exec("DELETE FROM sys_menus WHERE path = ?", "/admin/mail/campaign").Error; err != nil {
		t.Fatalf("构造报表行缺失态失败: %v", err)
	}
	if _, still := mailSplitMenuByPath(t, db, "/admin/mail/campaign"); still {
		t.Fatal("报表行未被删除，反例 f 不成立")
	}
	if got := mailSplitDecision(t, db); got != 1 {
		t.Errorf("报表行缺失时应视为满足（NOT EXISTS 语义），判定应返回 1，实际 %d", got)
	}

	cond := mailSplitSeed(t).ConditionSQL
	if strings.Contains(cond, "$1") || strings.Contains(cond, "?") {
		t.Errorf("判定 SQL 含参数占位符（db.Raw 不做参数替换，会报语法错）:\n%s", cond)
	}
}

type mailSplitTimedMenuRow struct {
	ID         int64
	Path       string
	SortOrder  int
	IsHidden   int
	IsSystem   int
	IsPublic   int
	UpdateTime time.Time
}

func mailSplitTimedSnapshot(t *testing.T, db *gorm.DB) []mailSplitTimedMenuRow {
	t.Helper()
	var rows []mailSplitTimedMenuRow
	if err := db.Raw(`SELECT id, coalesce(path, '') AS path, sort_order, is_hidden, is_system, is_public,
			coalesce(update_time, to_timestamp(0)) AS update_time
		FROM sys_menus WHERE deleted_at IS NULL ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatalf("读取 sys_menus 时间快照失败: %v", err)
	}
	return rows
}

func TestMailSplitMenuReplayAffectsNoRows(t *testing.T) {
	db := mailSplitDB(t)
	mailSplitApplySeeds(t, db)

	before := mailSplitTimedSnapshot(t, db)
	if len(before) == 0 {
		t.Fatal("种子执行后 sys_menus 为空：前置不成立")
	}

	stmts := migrations.SplitStatements(mailSplitSeed(t).SQL)
	if len(stmts) == 0 {
		t.Fatal("521 的 SQL 拆分后为空：注册内容异常")
	}
	for _, stmt := range stmts {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		res := db.Exec(stmt)
		if res.Error != nil {
			t.Fatalf("重放语句失败（幂等性不成立）: %v\nSQL: %s", res.Error, stmt)
		}
		if res.RowsAffected != 0 {
			t.Errorf("完整态下重放语句影响了 %d 行，应为 0 行（WHERE / NOT EXISTS 守卫没挡住）:\nSQL: %s",
				res.RowsAffected, stmt)
		}
	}

	after := mailSplitTimedSnapshot(t, db)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("重放后含 update_time 的全表快照发生变化（有语句真的写入了行）:\n before=%+v\n  after=%+v",
			before, after)
	}
}
