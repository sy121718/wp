package feature

import (
	"reflect"
	"regexp"
	"testing"

	"gorm.io/gorm"

	migrations "go_wp/public/migrations"
	"go_wp/public/test/support"
)

const (
	mailContactCrudPermissionSeedVersion = "525-mail-contact-crud-permission"

	mailContactCrudI18nSeedVersion = "526-mail-contact-crud-i18n"

	mailContactCrudMenuPath = "/admin/mail/contacts"
)

var mailContactCrudPermissions = []struct {
	code   string
	name   string
	path   string
	method string
}{
	{"mail:contact_save", "保存联系人", "/api/mail/contact/save", "POST"},
	{"mail:contact_delete", "删除联系人", "/api/mail/contact/delete", "POST"},
	{"mail:contact_tag", "批量打标签", "/api/mail/contact/tag", "POST"},
}

var mailContactCrudButtons = []struct {
	title string
	code  string
	sort  int
}{
	{"保存联系人", "mail:contact_save", 68},
	{"删除联系人", "mail:contact_delete", 69},
	{"批量打标签", "mail:contact_tag", 70},
}

var mailContactCrudI18nPairRe = regexp.MustCompile(`\('([a-zA-Z0-9_.]+)',\s*'(zh-CN|en-US)',`)

const mailContactCrudI18nMinPairs = 50

func mailContactCrudSeed(t *testing.T, version string) migrations.Seed {
	t.Helper()
	for _, s := range migrations.AllSeeds() {
		if s.Version == version {
			return s
		}
	}
	t.Fatalf("种子 %s 未注册到 migrations.AllSeeds()", version)
	return migrations.Seed{}
}

func mailContactCrudDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}
	for _, table := range []string{"sys_permission", "sys_menus", "sys_i18n"} {
		var regclass *string
		if err := db.Raw("SELECT to_regclass(?)::text", table).Scan(&regclass).Error; err != nil {
			t.Fatalf("检查 %s 是否存在失败: %v", table, err)
		}
		if regclass == nil || *regclass == "" {
			t.Fatalf("测试库缺少 %s 表：生产迁移未生效", table)
		}
	}
	return db
}

func mailContactCrudApplySeeds(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行全部种子失败: %v", err)
	}
}

func mailContactCrudReplay(t *testing.T, db *gorm.DB, version string) {
	t.Helper()
	stmts := migrations.SplitStatements(mailContactCrudSeed(t, version).SQL)
	if len(stmts) == 0 {
		t.Fatalf("%s 的 SQL 被拆成 0 条语句（文件空了？判定门槛会静默失效）", version)
	}
	for i, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放 %s 第 %d 条语句失败: %v\n语句：%s", version, i+1, err, stmt)
		}
	}
}

func mailContactCrudDecision(t *testing.T, db *gorm.DB, version string) int64 {
	t.Helper()
	var got int64
	if err := db.Raw(mailContactCrudSeed(t, version).ConditionSQL).Scan(&got).Error; err != nil {
		t.Fatalf("执行 %s 判定 SQL 失败: %v", version, err)
	}
	return got
}

func mailContactCrudI18nPairs(t *testing.T) [][2]string {
	t.Helper()
	seed := mailContactCrudSeed(t, mailContactCrudI18nSeedVersion)
	var pairs [][2]string
	for _, stmt := range migrations.SplitStatements(seed.SQL) {
		if !regexp.MustCompile(`INSERT INTO sys_i18n`).MatchString(stmt) {
			continue
		}
		for _, m := range mailContactCrudI18nPairRe.FindAllStringSubmatch(stmt, -1) {
			pairs = append(pairs, [2]string{m[1], m[2]})
		}
	}
	return pairs
}

func TestMailContactCrudPermissionSeedAppliesExpectedStructure(t *testing.T) {
	db := mailContactCrudDB(t)
	mailContactCrudApplySeeds(t, db)

	var menuID int64
	if err := db.Raw(`SELECT id FROM sys_menus
		WHERE path = ? AND type = 2 AND deleted_at IS NULL
		ORDER BY id DESC LIMIT 1`, mailContactCrudMenuPath).Scan(&menuID).Error; err != nil {
		t.Fatalf("读取「联系人」菜单失败: %v", err)
	}
	if menuID == 0 {
		t.Fatalf("「联系人」菜单（%s）不存在：521 未生效，按钮将无处挂载", mailContactCrudMenuPath)
	}

	for _, want := range mailContactCrudPermissions {
		var got struct {
			Name   string
			Module string
			Path   string
			Method string
			Status int
		}
		if err := db.Raw(`SELECT permission_name AS name, module, api_path AS path,
				api_method AS method, status
			FROM sys_permission WHERE permission_code = ?`, want.code).Scan(&got).Error; err != nil {
			t.Fatalf("读取权限点 %s 失败: %v", want.code, err)
		}
		if got.Path != want.path || got.Method != want.method {
			t.Errorf("权限点 %s 的接口应为 %s %s，实际 %q %q",
				want.code, want.method, want.path, got.Method, got.Path)
		}
		if got.Name != want.name || got.Module != "mail" || got.Status != 1 {
			t.Errorf("权限点 %s 的落库值不对：name=%q module=%q status=%d（期望 %q/mail/1）",
				want.code, got.Name, got.Module, got.Status, want.name)
		}
	}

	for _, want := range mailContactCrudButtons {
		var got struct {
			Title   string
			Parent  int64
			Type    int
			Sort    int
			Deleted *string
		}
		if err := db.Raw(`SELECT title, coalesce(parent_id, 0) AS parent, type,
				coalesce(sort_order, 0) AS sort, deleted_at::text AS deleted
			FROM sys_menus WHERE permission_code = ? AND type = 3
			ORDER BY id DESC LIMIT 1`, want.code).Scan(&got).Error; err != nil {
			t.Fatalf("读取按钮 %s 失败: %v", want.code, err)
		}
		if got.Title != want.title || got.Type != 3 || got.Sort != want.sort {
			t.Errorf("按钮 %s 应为 type=3 sort=%d title=%q，实际 type=%d sort=%d title=%q",
				want.code, want.sort, want.title, got.Type, got.Sort, got.Title)
		}
		if got.Parent != menuID {
			t.Errorf("按钮 %s 的父菜单应为「联系人」(%d)，实际 %d（挂错模块 = 运营找不到入口）",
				want.code, menuID, got.Parent)
		}
	}

	if got := mailContactCrudDecision(t, db, mailContactCrudPermissionSeedVersion); got != 1 {
		t.Errorf("525 判定应为 1（全部对象到位），实际 %d", got)
	}
}

func TestMailContactCrudPermissionSeedIsIdempotent(t *testing.T) {
	db := mailContactCrudDB(t)
	mailContactCrudApplySeeds(t, db)

	type row struct {
		Title  string
		Type   int
		Path   string
		Sort   int
		Parent int64
		Code   string
	}
	snapshot := func() []row {
		t.Helper()
		var rows []row
		if err := db.Raw(`SELECT title, type, coalesce(path, '') AS path,
				coalesce(sort_order, 0) AS sort, coalesce(parent_id, 0) AS parent,
				coalesce(permission_code, '') AS code
			FROM sys_menus WHERE deleted_at IS NULL ORDER BY id`).Scan(&rows).Error; err != nil {
			t.Fatalf("读取 sys_menus 快照失败: %v", err)
		}
		return rows
	}
	before := snapshot()
	if len(before) == 0 {
		t.Fatal("种子执行后 sys_menus 为空：前置不成立")
	}

	mailContactCrudApplySeeds(t, db)
	if got := snapshot(); !reflect.DeepEqual(before, got) {
		t.Errorf("再次执行种子后 sys_menus 发生变化（判定跳过失效）：\n before=%+v\n  after=%+v", before, got)
	}

	mailContactCrudReplay(t, db, mailContactCrudPermissionSeedVersion)
	after := snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Errorf("重放 525 后 sys_menus 发生变化（SQL 不幂等）：\n before=%+v\n  after=%+v", before, after)
	}

	for _, want := range mailContactCrudButtons {
		var n int64
		if err := db.Raw(`SELECT count(*) FROM sys_menus
			WHERE permission_code = ? AND type = 3 AND deleted_at IS NULL`, want.code).Scan(&n).Error; err != nil {
			t.Fatalf("统计按钮 %s 失败: %v", want.code, err)
		}
		if n != 1 {
			t.Errorf("重放后按钮 %s 应仍只有 1 行，实际 %d 行（重复插行）", want.code, n)
		}
	}
	for _, want := range mailContactCrudPermissions {
		var n int64
		if err := db.Raw(`SELECT count(*) FROM sys_permission WHERE permission_code = ?`,
			want.code).Scan(&n).Error; err != nil {
			t.Fatalf("统计权限点 %s 失败: %v", want.code, err)
		}
		if n != 1 {
			t.Errorf("重放后权限点 %s 应仍只有 1 行，实际 %d 行", want.code, n)
		}
	}
	if got := mailContactCrudDecision(t, db, mailContactCrudPermissionSeedVersion); got != 1 {
		t.Errorf("重放后 525 判定应仍为 1，实际 %d", got)
	}
}

func TestMailContactCrudPermissionConditionRejectsPartialStates(t *testing.T) {
	db := mailContactCrudDB(t)
	mailContactCrudApplySeeds(t, db)

	if err := db.Exec(`DELETE FROM sys_menus WHERE permission_code = ? AND type = 3`,
		"mail:contact_tag").Error; err != nil {
		t.Fatalf("删除按钮失败: %v", err)
	}
	if got := mailContactCrudDecision(t, db, mailContactCrudPermissionSeedVersion); got != 0 {
		t.Errorf("少了「批量打标签」按钮时判定应为 0，实际 %d（半成品会被误判成已完成）", got)
	}

	mailContactCrudApplySeeds(t, db)
	var n int64
	if err := db.Raw(`SELECT count(*) FROM sys_menus
		WHERE permission_code = ? AND type = 3 AND deleted_at IS NULL`, "mail:contact_tag").Scan(&n).Error; err != nil {
		t.Fatalf("统计按钮失败: %v", err)
	}
	if n != 1 {
		t.Errorf("门槛返回 0 后再次执行种子，按钮应被补回 1 行，实际 %d 行", n)
	}
	if got := mailContactCrudDecision(t, db, mailContactCrudPermissionSeedVersion); got != 1 {
		t.Errorf("补齐后判定应回到 1，实际 %d", got)
	}

	if err := db.Exec(`DELETE FROM sys_permission WHERE permission_code = ?`,
		"mail:contact_delete").Error; err != nil {
		t.Fatalf("删除权限点失败: %v", err)
	}
	if got := mailContactCrudDecision(t, db, mailContactCrudPermissionSeedVersion); got != 0 {
		t.Errorf("少了权限点时判定应为 0，实际 %d", got)
	}
}

func TestMailContactCrudI18nSeedAppliesExpectedStructure(t *testing.T) {
	db := mailContactCrudDB(t)
	mailContactCrudApplySeeds(t, db)

	pairs := mailContactCrudI18nPairs(t)
	if len(pairs) < mailContactCrudI18nMinPairs {
		t.Fatalf("526 的 INSERT 元组只解析出 %d 对 (key, lang)，少于 %d —— 词条被删了？",
			len(pairs), mailContactCrudI18nMinPairs)
	}
	for _, pair := range pairs {
		var value string
		if err := db.Raw(`SELECT coalesce(item_value, '') FROM sys_i18n
			WHERE item_key = ? AND lang = ?`, pair[0], pair[1]).Scan(&value).Error; err != nil {
			t.Fatalf("读取词条 %s/%s 失败: %v", pair[0], pair[1], err)
		}
		if value == "" {
			t.Errorf("词条 %s/%s 没有落库（页面上会显示裸 key）", pair[0], pair[1])
		}
	}

	for _, want := range []struct{ lang, value string }{
		{"zh-CN", "新建一位联系人手工加一条，或用「导入联系人」批量导入 CSV —— 名单建立起来才能按标签圈人群群发。"},
		{"en-US", "Add one contact by hand, or import a CSV in bulk — the list has to exist before you can target people by tag."},
	} {
		var got string
		if err := db.Raw(`SELECT coalesce(item_value, '') FROM sys_i18n
			WHERE item_key = 'admin.mail.marketing.contacts.empty.initial' AND lang = ?`,
			want.lang).Scan(&got).Error; err != nil {
			t.Fatalf("读取空态描述 %s 失败: %v", want.lang, err)
		}
		if got != want.value {
			t.Errorf("空态描述 %s 的终值不对：\n got=%q\nwant=%q", want.lang, got, want.value)
		}
	}

	if got := mailContactCrudDecision(t, db, mailContactCrudI18nSeedVersion); got != 1 {
		t.Errorf("526 判定应为 1，实际 %d", got)
	}
}

func TestMailContactCrudI18nSeedIsIdempotent(t *testing.T) {
	db := mailContactCrudDB(t)
	mailContactCrudApplySeeds(t, db)

	pairs := mailContactCrudI18nPairs(t)
	before := map[[2]string]string{}
	for _, pair := range pairs {
		var got struct {
			Value     string
			UpdatedAt string
		}
		if err := db.Raw(`SELECT coalesce(item_value, '') AS value, update_time::text AS updated_at
			FROM sys_i18n WHERE item_key = ? AND lang = ?`, pair[0], pair[1]).Scan(&got).Error; err != nil {
			t.Fatalf("读取词条 %s/%s 失败: %v", pair[0], pair[1], err)
		}
		if got.Value == "" {
			t.Fatalf("词条 %s/%s 未落库，前置不成立", pair[0], pair[1])
		}
		before[pair] = got.Value + "|" + got.UpdatedAt
	}

	mailContactCrudApplySeeds(t, db)
	mailContactCrudReplay(t, db, mailContactCrudI18nSeedVersion)

	for pair, want := range before {
		var got struct {
			Value     string
			UpdatedAt string
		}
		if err := db.Raw(`SELECT coalesce(item_value, '') AS value, update_time::text AS updated_at
			FROM sys_i18n WHERE item_key = ? AND lang = ?`, pair[0], pair[1]).Scan(&got).Error; err != nil {
			t.Fatalf("重放后读取词条 %s/%s 失败: %v", pair[0], pair[1], err)
		}
		if got.Value+"|"+got.UpdatedAt != want {
			t.Errorf("重放后词条 %s/%s 被改动：\n got=%q\nwant=%q", pair[0], pair[1], got.Value+"|"+got.UpdatedAt, want)
		}
	}

	var total int64
	if err := db.Raw(`SELECT count(*) FROM sys_i18n WHERE item_key IN (
		'admin.mail.marketing.contact_form.new', 'mail.err.contactEmailExists',
		'mail.err.contactTagEmpty')`).Scan(&total).Error; err != nil {
		t.Fatalf("统计词条失败: %v", err)
	}
	if total != 6 {
		t.Errorf("抽查的三个键应有 6 行（3 键 × 2 语言），实际 %d 行（重复插行）", total)
	}
	if got := mailContactCrudDecision(t, db, mailContactCrudI18nSeedVersion); got != 1 {
		t.Errorf("重放后 526 判定应仍为 1，实际 %d", got)
	}
}
