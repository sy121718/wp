package feature

import (
	"fmt"
	"regexp"
	"testing"

	migrations "go_wp/public/migrations"
	"go_wp/public/test/support"
)

func TestRetiredSiteSlotUISeedIsPreciseAndIdempotent(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	var seed migrations.Seed
	for _, candidate := range migrations.AllSeeds() {
		if candidate.Version == "689-retired-site-slot-ui" {
			seed = candidate
			break
		}
	}
	if seed.Version == "" {
		t.Fatal("旧槽位UI清退seed未注册")
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatal(err)
	}
	keys := regexp.MustCompile(`'((?:admin\.site_slots\.[A-Za-z0-9_.]+|admin\.theme\.site_slots_link))'`).FindAllStringSubmatch(seed.SQL, -1)
	if len(keys) == 0 {
		t.Fatal("清退SQL没有明确词条集合")
	}
	for _, key := range keys {
		if err := db.Exec(`INSERT INTO sys_i18n (item_key,lang,item_value,http_code,category,status,create_time,update_time) VALUES (?, 'zh-CN', '旧槽位词条',200,'admin',1,now(),now()) ON CONFLICT (item_key,lang) DO NOTHING`, key[1]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []string{"page:site_slot_list", "page:site_slot_bind", "page:site_slot_unbind"} {
		if err := db.Exec(`INSERT INTO sys_permission (permission_code,permission_name,module,api_path,api_method) VALUES (?, '旧槽位权限','page',?, 'POST') ON CONFLICT (permission_code) DO NOTHING`, code, "/retired-slot-ui/"+code).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3) VALUES ('p','retired-slot-ui','/old-slot-ui','POST',?)`, code).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/site-slots", "/admin/site-slots"} {
		if err := db.Exec(`INSERT INTO sys_menus (title,type,path,permission_code) VALUES ('旧槽位菜单',2,?,'page:site_slot_list')`, path).Error; err != nil {
			t.Fatal(err)
		}
	}

	for _, statement := range []string{
		`INSERT INTO sys_i18n (item_key,lang,item_value,http_code,category,status) VALUES ('admin.site_slots.probe_keep','zh-CN','保留词条',200,'admin',1)`,
		`INSERT INTO sys_permission (permission_code,permission_name,module,api_path,api_method) VALUES ('page:site_slot_probe_keep','保留权限','page','/retired-slot-probe-keep','GET')`,
		`INSERT INTO sys_casbin_rule (ptype,v0,v1,v2,v3) VALUES ('g','retired-slot-ui','keep','GET','page:site_slot_list')`,
		`INSERT INTO sys_menus (title,type,path,permission_code) VALUES ('保留菜单',2,'/custom-slot-keep','page:site_slot_list')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	decision := func() int {
		t.Helper()
		var got int
		if err := db.Raw(seed.ConditionSQL).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := decision(); got != 0 {
		t.Fatalf("存在旧专用对象时不得跳过清退: %d", got)
	}

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	for index, statement := range migrations.SplitStatements(seed.SQL) {
		name := fmt.Sprintf("retired_slot_ui_%d", index)
		if err := tx.Exec("PREPARE " + name + " AS " + statement).Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.Exec("DEALLOCATE " + name).Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if got := decision(); got != 1 {
		t.Fatalf("清退后精确集合须全空: %d", got)
	}
	var preserved int64
	if err := db.Raw(`SELECT (SELECT count(*) FROM sys_i18n WHERE item_key='admin.site_slots.probe_keep' AND item_value='保留词条') + (SELECT count(*) FROM sys_permission WHERE permission_code='page:site_slot_probe_keep') + (SELECT count(*) FROM sys_casbin_rule WHERE ptype='g' AND v0='retired-slot-ui') + (SELECT count(*) FROM sys_menus WHERE path='/custom-slot-keep' AND title='保留菜单')`).Scan(&preserved).Error; err != nil {
		t.Fatal(err)
	}
	if preserved != 4 {
		t.Fatalf("本批以外的四类对象须完整保留: %d", preserved)
	}
	for _, statement := range migrations.SplitStatements(seed.SQL) {
		result := db.Exec(statement)
		if result.Error != nil || result.RowsAffected != 0 {
			t.Fatalf("直接重放须无额外改动: affected=%d err=%v", result.RowsAffected, result.Error)
		}
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatal(err)
	}
	if got := decision(); got != 1 {
		t.Fatalf("重跑全部现行seed不得复活旧槽位对象: %d", got)
	}
}
