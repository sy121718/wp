package migrations_test

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// artex（真实开发库）上 723 被守卫拒绝的形态：
//   - admin.common.action.preview 与 uiActPrev 文案相同，只有播种 remark 不同；
//   - admin.products.row.preview 是 316 裁决后的「预览」，pRowPrev 是机械改名带回来的
//     「预览详情页」（191 原始文案）。
//
// 722z 只修「旧行 = 316 裁决值，且新行仍是机械默认值（无编辑痕迹）或已是裁决值」；
// 其余交 723 按原严格语义拒绝。

func previewRepairSeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, s := range migrations.AllSeeds() {
		if s.Version == "722z-product-preview-retired-key-repair" {
			return s
		}
	}
	t.Fatal("722z 预览词条修复 seed 未登记")
	return migrations.Seed{}
}

func productRemainingShortKeySeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, s := range migrations.AllSeeds() {
		if s.Version == "723-product-remaining-short-keys-i18n" {
			return s
		}
	}
	t.Fatal("723 商品剩余短键 seed 未登记")
	return migrations.Seed{}
}

// seedPreviewConflict 复刻 artex 的四行状态：旧键为 316 裁决文案，新键为 723 机械默认值。
func seedPreviewConflict(t *testing.T, db *gorm.DB) {
	t.Helper()
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key IN ('admin.products.row.preview','admin.common.action.preview','pRowPrev','uiActPrev')")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key,lang,item_value,http_code,category,remark,status,create_time,update_time)
VALUES
 ('admin.products.row.preview','zh-CN','预览','200','admin','admin/products.html',1,'2026-10-24 21:19:48.150139+08','2026-10-24 21:19:48.339843+08'),
 ('admin.products.row.preview','en-US','Preview','200','admin','admin/products.html',1,'2026-10-24 21:19:48.150139+08','2026-10-24 21:19:48.339843+08'),
 ('admin.common.action.preview','zh-CN','预览','200','admin','通用动作：预览（行内不重复实体名）',1,'2026-10-24 21:19:48.238807+08','2026-10-24 21:19:48.238807+08'),
 ('admin.common.action.preview','en-US','Preview','200','admin','通用动作：预览（行内不重复实体名）',1,'2026-10-24 21:19:48.238807+08','2026-10-24 21:19:48.238807+08'),
 ('pRowPrev','zh-CN','预览详情页','200','admin','admin/products.html',1,'2026-11-03 14:06:59.168344+08','2026-11-03 14:06:59.168344+08'),
 ('pRowPrev','en-US','Preview detail page','200','admin','admin/products.html',1,'2026-11-03 14:06:59.168344+08','2026-11-03 14:06:59.168344+08'),
 ('uiActPrev','zh-CN','预览','200','admin','第四轮后台页面改造新增文案位',1,'2026-11-03 14:06:59.208497+08','2026-11-03 14:06:59.208497+08'),
 ('uiActPrev','en-US','Preview','200','admin','第四轮后台页面改造新增文案位',1,'2026-11-03 14:06:59.208497+08','2026-11-03 14:06:59.208497+08')`)
}

func applySeedSQL(db *gorm.DB, s migrations.Seed) error {
	return db.Exec(s.SQL).Error
}

func previewValues(t *testing.T, db *gorm.DB, key string) map[string]string {
	t.Helper()
	var rows []struct {
		Lang      string
		ItemValue string
	}
	if err := db.Raw("SELECT lang, item_value FROM sys_i18n WHERE item_key = ?", key).Scan(&rows).Error; err != nil {
		t.Fatalf("读取 %s 失败: %v", key, err)
	}
	out := map[string]string{}
	for _, r := range rows {
		out[r.Lang] = r.ItemValue
	}
	return out
}

// TestPreviewRetiredKeyRepairSettlesArtexShape 复现 artex 冲突：
// 修复前 723 必须拒绝并定位，修复后 723 通过、旧键归零、新键是 316 的短文案。
func TestPreviewRetiredKeyRepairSettlesArtexShape(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	repair := previewRepairSeed(t)
	seed723 := productRemainingShortKeySeed(t)
	seedPreviewConflict(t, db)

	// 前提：不修复时 723 必须整批拒绝并给出可定位的 old -> new/lang。
	err := applySeedSQL(db, seed723)
	if err == nil {
		t.Fatal("前提不成立：这个形态本该被 723 判为冲突")
	}
	for _, want := range []string{
		"admin.products.row.preview -> pRowPrev/zh-CN",
		"admin.products.row.preview -> pRowPrev/en-US",
		"admin.common.action.preview -> uiActPrev/zh-CN",
		"admin.common.action.preview -> uiActPrev/en-US",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("冲突必须定位 %q，实际：%v", want, err)
		}
	}

	// 修复执行后再跑 723。
	if err := applySeedSQL(db, repair); err != nil {
		t.Fatalf("722z 执行失败: %v", err)
	}
	if err := applySeedSQL(db, seed723); err != nil {
		t.Fatalf("修复后 723 仍然冲突: %v", err)
	}

	for _, oldKey := range []string{"admin.products.row.preview", "admin.common.action.preview"} {
		if left := previewValues(t, db, oldKey); len(left) != 0 {
			t.Fatalf("退役 key %s 应归零，实际: %v", oldKey, left)
		}
	}
	if got := previewValues(t, db, "pRowPrev"); got["zh-CN"] != "预览" || got["en-US"] != "Preview" {
		t.Fatalf("pRowPrev 应回到 316 的短文案，实际: %v", got)
	}
	if got := previewValues(t, db, "uiActPrev"); got["zh-CN"] != "预览" || got["en-US"] != "Preview" {
		t.Fatalf("uiActPrev 文案错误: %v", got)
	}
}

// TestPreviewRetiredKeyRepairLeavesEditedRowsAlone 运营改过的行不裁决：
// 新键被改成自定义文案且留有编辑痕迹时，修复必须跳过，723 仍然拒绝。
func TestPreviewRetiredKeyRepairLeavesEditedRowsAlone(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	repair := previewRepairSeed(t)
	seedPreviewConflict(t, db)
	mustExec(t, db, `UPDATE sys_i18n SET item_value = '自定义预览', update_time = now() + interval '1 hour'
WHERE item_key = 'pRowPrev' AND lang = 'zh-CN'`)

	if err := applySeedSQL(db, repair); err != nil {
		t.Fatalf("722z 执行失败: %v", err)
	}
	if got := previewValues(t, db, "pRowPrev")["zh-CN"]; got != "自定义预览" {
		t.Fatalf("有编辑痕迹的新键被改写: %q", got)
	}
	if got := previewValues(t, db, "admin.products.row.preview")["zh-CN"]; got != "预览" {
		t.Fatalf("新键未修复时旧键不应被删: %q", got)
	}
	if err := applySeedSQL(db, productRemainingShortKeySeed(t)); err == nil {
		t.Fatal("未修复的冲突应由 723 继续拒绝")
	}
}

// TestPreviewRetiredKeyRepairFreshInstall 全新库只有机械默认值时同样回到 316 的短文案。
func TestPreviewRetiredKeyRepairFreshInstall(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	repair := previewRepairSeed(t)
	mustExec(t, db, "DELETE FROM sys_i18n WHERE item_key IN ('pRowPrev','uiActPrev')")
	mustExec(t, db, `INSERT INTO sys_i18n (item_key,lang,item_value,http_code,category,remark,status,create_time,update_time)
VALUES ('pRowPrev','zh-CN','预览详情页','200','admin','admin/products.html',1,now(),now()),
       ('pRowPrev','en-US','Preview detail page','200','admin','admin/products.html',1,now(),now())`)

	if err := applySeedSQL(db, repair); err != nil {
		t.Fatalf("722z 执行失败: %v", err)
	}
	if got := previewValues(t, db, "pRowPrev"); got["zh-CN"] != "预览" || got["en-US"] != "Preview" {
		t.Fatalf("全新库应得到 316 的短文案，实际: %v", got)
	}

	// 幂等：再跑一次不应再改动任何行。
	if err := applySeedSQL(db, repair); err != nil {
		t.Fatalf("722z 重复执行失败: %v", err)
	}
}
