package feature

import (
	"testing"

	"gorm.io/gorm"

	mediaenums "go_wp/internal/module/media/enums"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 本文件锁 716 这条 seed：文件分类（file category）与媒体列表游标的七条错误词条，
// 以及它们的 http_code 与 mediaenums.StatusOf 是否一致。
//
// 放在 feature 包（与 712 / 713 / 715 同理）：按 media 精确包可运行。
const fileCategoryI18nVersion = "716-file-category-i18n"

// fileCategoryKeys 是本批的七个 key，与 register 的 ConditionSQL 枚举的完全一致。
var fileCategoryKeys = []string{
	"ErrFileCategoryNameRequired",
	"ErrFileCategoryNotFound",
	"ErrFileCategoryParentNotFound",
	"ErrFileCategoryNameDuplicated",
	"ErrFileCategoryCycle",
	"ErrFileCategoryHasChildren",
	"ErrMediaCursorInvalid",
}

func findFileCategorySeed(t *testing.T) migrations.Seed {
	t.Helper()
	for _, seed := range migrations.AllSeeds() {
		if seed.Version == fileCategoryI18nVersion {
			return seed
		}
	}
	t.Fatalf("种子 %s 未注册（register.go 里漏了 registerFileCategoryI18n？）", fileCategoryI18nVersion)
	return migrations.Seed{}
}

// TestFileCategorySeedPrepares 按 public 规则做持久化 PREPARE 校验：
// 新 SQL 上线前必须能准备成功，否则那条迁移会在执行时才炸。
func TestFileCategorySeedPrepares(t *testing.T) {
	seed := findFileCategorySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("PREPARE file_category_seed AS " + seed.SQL).Error; err != nil {
			return err
		}
		return conn.Exec("DEALLOCATE file_category_seed").Error
	}); err != nil {
		t.Fatal(err)
	}
}

// TestFileCategorySeedCoversAllKeysAndLanguages 钉住三件事：
//  1. 七个 key 的中英两条全齐（十四行），文案非空、不是裸 key；
//  2. 词库里的 http_code 与 mediaenums.StatusOf 声明的状态一致 —— 状态码的事实源
//     在 Go 声明，词库是可观测副本，两边分叉时用户看到的 code 和文案会互相矛盾；
//  3. 门槛在完整数据上为 1。
func TestFileCategorySeedCoversAllKeysAndLanguages(t *testing.T) {
	seed := findFileCategorySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("跑 seed 失败：%v", err)
	}

	type row struct {
		ItemKey  string
		Lang     string
		Value    string
		HttpCode int
	}
	var rows []row
	if err := db.Table("sys_i18n").
		Select("item_key, lang, item_value AS value, http_code").
		Where("item_key IN ?", fileCategoryKeys).
		Order("item_key, lang").
		Scan(&rows).Error; err != nil {
		t.Fatalf("读词条失败：%v", err)
	}
	if len(rows) != len(fileCategoryKeys)*2 {
		t.Fatalf("应有 %d 条（七 key × 中英），实际 %d 条：%+v", len(fileCategoryKeys)*2, len(rows), rows)
	}

	seen := make(map[string]map[string]row, len(fileCategoryKeys))
	for _, r := range rows {
		byLang, ok := seen[r.ItemKey]
		if !ok {
			byLang = make(map[string]row, 2)
			seen[r.ItemKey] = byLang
		}
		if _, dup := byLang[r.Lang]; dup {
			t.Errorf("%s/%s 出现重复行", r.ItemKey, r.Lang)
		}
		byLang[r.Lang] = r
	}
	for _, key := range fileCategoryKeys {
		byLang, ok := seen[key]
		if !ok {
			t.Fatalf("%s 一条词条都没有", key)
		}
		for _, lang := range []string{"zh-CN", "en-US"} {
			r, ok := byLang[lang]
			if !ok {
				t.Errorf("%s 缺 %s", key, lang)
				continue
			}
			if r.Value == "" || r.Value == key {
				t.Errorf("%s/%s 文案不能为空、也不能是裸 key：%q", key, lang, r.Value)
			}
			// 英文行里出现中文字符说明是抄错了语言，不是真的翻译。
			if lang == "en-US" {
				for _, ch := range r.Value {
					if ch >= 0x4E00 && ch <= 0x9FFF {
						t.Errorf("%s/en-US 里混进了中文：%q", key, r.Value)
						break
					}
				}
			}
		}
	}

	// ② http_code 与 StatusOf 一致（同一 key 的中英两行也必须一致）。
	for _, key := range fileCategoryKeys {
		want := fileCategoryStatusOf(t, key)
		for _, lang := range []string{"zh-CN", "en-US"} {
			if got := seen[key][lang].HttpCode; got != want {
				t.Errorf("%s/%s http_code = %d，与 StatusOf 声明的 %d 不一致", key, lang, got, want)
			}
		}
	}

	// ③ 完整数据上门槛为 1。
	if got := fileCategoryGate(t, db, seed); got != "1" {
		t.Fatalf("十四行齐备时门槛应为 1，实际 %q", got)
	}
}

// TestFileCategorySeedGateRejectsPartialData 是门槛的反例表：删掉任意一行，
// 门槛必须变 0。逐行删而不是只删一条 —— 只验一条时，若 ConditionSQL 里的元组写错
// 了位置（比如漏了某个 key），删其它 key 的行仍然会是 1，缺陷就漏过去了。
func TestFileCategorySeedGateRejectsPartialData(t *testing.T) {
	seed := findFileCategorySeed(t)
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("跑 seed 失败：%v", err)
	}

	for _, key := range fileCategoryKeys {
		for _, lang := range []string{"zh-CN", "en-US"} {
			t.Run(key+"/"+lang, func(t *testing.T) {
				// 每条子例都在同一个库上跑，删完必须补回去（RunSeeds 幂等，但重跑一次
				// 更慢；这里用事务内回滚最省事 —— 门槛 SQL 与 INSERT 都在同一库上看得到）。
				tx := db.Begin()
				if tx.Error != nil {
					t.Fatalf("开事务失败：%v", tx.Error)
				}
				defer func() { _ = tx.Rollback() }()

				if err := tx.Exec("DELETE FROM sys_i18n WHERE item_key = ? AND lang = ?", key, lang).Error; err != nil {
					t.Fatalf("删 %s/%s 失败：%v", key, lang, err)
				}
				var native string
				if err := tx.Raw(seed.ConditionSQL).Scan(&native).Error; err != nil {
					t.Fatalf("求值门槛失败：%v", err)
				}
				if native != "0" {
					t.Fatalf("删掉 %s/%s 后门槛必须为 0，实际 %q —— ConditionSQL 没有精确枚举这个元组", key, lang, native)
				}
			})
		}
	}
}

// fileCategoryStatusOf 从 Go 声明里取该 key 的状态码。
// 用 mediaenums.StatusOf 而不是在测试里再抄一份数字：状态码的事实源只有一处。
func fileCategoryStatusOf(t *testing.T, key string) int {
	t.Helper()
	status := mediaenums.StatusOf(key)
	if status == 0 {
		t.Fatalf("%s 在 mediaenums.StatusOf 里没有声明状态码", key)
	}
	return status
}

func fileCategoryGate(t *testing.T, db *gorm.DB, seed migrations.Seed) string {
	t.Helper()
	var native string
	if err := db.Raw(seed.ConditionSQL).Scan(&native).Error; err != nil {
		t.Fatalf("求值门槛失败：%v", err)
	}
	return native
}
