package i18n_test

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/pkg/database"
	"go_wp/pkg/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	adminenums "go_wp/internal/module/admin/enums"
	pageenums "go_wp/internal/module/page/enums"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func i18nTestDSN(dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		support.DefaultPGHost, support.DefaultPGUser, support.DefaultPGPassword, dbname, support.DefaultPGPort)
}

func newI18nTestConfig(dbname string) *viper.Viper {
	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", dbname)
	cfg.Set("database.host", support.DefaultPGHost)
	cfg.Set("database.port", 5432)
	cfg.Set("database.user", support.DefaultPGUser)
	cfg.Set("database.password", support.DefaultPGPassword)
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 2)
	cfg.Set("i18n.default_lang", "zh-CN")
	cfg.Set("i18n.auto_refresh", false)
	return cfg
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func countRows(t *testing.T, db *gorm.DB, table, where string, args ...any) int64 {
	t.Helper()
	var n int64
	query := db.Table(table)
	if where != "" {
		query = query.Where(where, args...)
	}
	if err := query.Count(&n).Error; err != nil {
		t.Fatalf("统计 %s 行数失败: %v", table, err)
	}
	return n
}

func columnCount(t *testing.T, db *gorm.DB, table string, columns ...string) int64 {
	t.Helper()
	var n int64
	err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND column_name IN (?)`,
		table, columns).Scan(&n).Error
	if err != nil {
		t.Fatalf("查询 %s 列失败: %v", table, err)
	}
	return n
}

// onlyStringPlaceholders 判「这句话能不能被纯字符串参数填满」。
//
// 取代已收口掉的 i18n.HasStringPlaceholdersOnly，判据不变：StringPlaceholderCount
// 的 ok=false 恰好覆盖 %d / %v / %[1]s / 宽度精度星号 / 裸百分号这些会让 fmt 吐标记的
// 形态（pkg/i18n/placeholder.go 说它就是唯一扫描器）。这里只看 ok，不看数量 ——
// 数量由各用例自己另行断言。
func onlyStringPlaceholders(s string) bool {
	_, ok := i18n.StringPlaceholderCount(s)
	return ok
}

func TestI18nEnumsSeedSchemaAndIdempotency(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		support.FailIfRequiredPG(t, err)
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}

	for round := 1; round <= 2; round++ {
		if err := migrations.Run(db); err != nil {
			t.Fatalf("第 %d 轮结构迁移失败: %v", round, err)
		}
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮 seed 失败: %v", round, err)
		}
	}

	if got := columnCount(t, db, "sys_i18n", "category", "remark"); got != 2 {
		t.Fatalf("sys_i18n 应存在 category/remark 两列，实际 %d", got)
	}

	if got := countRows(t, db, "sys_i18n_revision", ""); got != 1 {
		t.Fatalf("sys_i18n_revision 应为单行，实际 %d 行", got)
	}

	if got := columnCount(t, db, "sys_menus", "title_key"); got != 1 {
		t.Fatalf("sys_menus 应存在 title_key 列，实际 %d", got)
	}

	const minEnglishKeys = 4240
	zhCount := countRows(t, db, "sys_i18n", "lang = ?", "zh-CN")
	enCount := countRows(t, db, "sys_i18n", "lang = ?", "en-US")
	t.Logf("sys_i18n 干净库词条：zh-CN=%d en-US=%d", zhCount, enCount)

	if enCount < minEnglishKeys || zhCount < minEnglishKeys {
		t.Fatalf("sys_i18n 中英词条低于已验证基线：zh-CN=%d en-US=%d，基线至少 %d", zhCount, enCount, minEnglishKeys)
	}

	var languageDiff []struct {
		Side    string
		ItemKey string
	}
	if err := db.Raw(`SELECT 'zh-only' AS side, item_key FROM
		(SELECT item_key FROM sys_i18n WHERE lang = 'zh-CN' EXCEPT SELECT item_key FROM sys_i18n WHERE lang = 'en-US') z
		UNION ALL SELECT 'en-only' AS side, item_key FROM
		(SELECT item_key FROM sys_i18n WHERE lang = 'en-US' EXCEPT SELECT item_key FROM sys_i18n WHERE lang = 'zh-CN') e
		ORDER BY side, item_key`).Scan(&languageDiff).Error; err != nil {
		t.Fatalf("查询中英差异键失败: %v", err)
	}
	if len(languageDiff) > 0 {
		t.Fatalf("sys_i18n 出现单侧 key（中英必须一一对应）：%v", languageDiff)
	}

	if zhCount != enCount {
		t.Fatalf("sys_i18n 中英行数不等：zh-CN=%d en-US=%d", zhCount, enCount)
	}

	for _, key := range []string{
		"admin.mail.marketing.contacts.empty.initial.title",
		"admin.mail.marketing.contacts.empty.initial",
		"admin.inventory.moves.title",
		"admin.media.bulk.delete",
		"admin.media.heading",
		"admin.article.edit.unavailable",
	} {
		if got := countRows(t, db, "sys_i18n", "item_key = ? AND lang IN ?", key, []string{"zh-CN", "en-US"}); got != 2 {
			t.Fatalf("%s 应有完整中英词条，实际 %d 行", key, got)
		}
	}

	latestSeeds := map[string]bool{
		"440-i18n-product-list-help":       false,
		"441-i18n-mail-empty-states":       false,
		"442-i18n-inventory-moves-heading": false,
		"447-i18n-missing-en-and-unseeded": false,
	}
	for _, seed := range migrations.AllSeeds() {
		if _, expected := latestSeeds[seed.Version]; !expected {
			continue
		}
		if latestSeeds[seed.Version] {
			t.Fatalf("重复注册 seed %s", seed.Version)
		}
		latestSeeds[seed.Version] = true
		var settled int64
		if err := db.Raw(seed.ConditionSQL).Scan(&settled).Error; err != nil {
			t.Fatalf("检查 seed %s 幂等判定失败: %v", seed.Version, err)
		}
		if settled <= 0 {
			t.Fatalf("seed %s 执行两轮后仍未满足幂等判定", seed.Version)
		}
	}
	for version, found := range latestSeeds {
		if !found {
			t.Fatalf("最新 seed %s 未注册到 AllSeeds", version)
		}
	}

	dimensionKeys := []string{
		"admin.analytics.referrers.title", "admin.analytics.referrers.hint",
		"admin.analytics.referrers.empty", "admin.analytics.referrers.unknown",
		"admin.analytics.ua.title", "admin.analytics.ua.hint",
		"admin.analytics.ua.empty", "admin.analytics.ua.unknown",
		"admin.analytics.langs.title", "admin.analytics.langs.hint",
		"admin.analytics.langs.empty", "admin.analytics.langs.unknown",
		"admin.analytics.rank.hint_lead", "admin.analytics.rank.hint_tail",
		"admin.analytics.col.referrer", "admin.analytics.col.ua_class",
		"admin.analytics.col.lang", "admin.analytics.breakdown.retention_hint",
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", dimensionKeys, "zh-CN"); got != 18 {
		t.Fatalf("222 的 18 个 key 应有 zh-CN 各一行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", dimensionKeys, "en-US"); got != 18 {
		t.Fatalf("222 的 18 个 key 应有 en-US 各一行（不许只写中文），实际 %d", got)
	}
	var dimensionPairs []struct {
		ItemKey string
		ZH      string
		EN      string
	}
	if err := db.Table("sys_i18n AS z").
		Select("z.item_key AS item_key, z.item_value AS zh, e.item_value AS en").
		Joins("JOIN sys_i18n e ON e.item_key = z.item_key AND e.lang = 'en-US'").
		Where("z.lang = 'zh-CN' AND z.item_key IN (?)", dimensionKeys).
		Scan(&dimensionPairs).Error; err != nil {
		t.Fatalf("查询 222 词条中英对失败: %v", err)
	}
	if len(dimensionPairs) != 18 {
		t.Fatalf("222 词条中英匹配应为 18 对，实际 %d 对", len(dimensionPairs))
	}
	for _, p := range dimensionPairs {
		if p.ZH == "" || p.EN == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if p.ZH == p.EN {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}
	}

	ruleKeys := []string{
		"ErrRuleConfigInvalid", "ErrRuleFieldNotAllowed",
		"ErrRuleLogicNotAllowed", "ErrRuleOpNotAllowed",
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", ruleKeys, "zh-CN"); got != 4 {
		t.Fatalf("226 的 4 个 key 应有 zh-CN 各一行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", ruleKeys, "en-US"); got != 4 {
		t.Fatalf("226 的 4 个 key 应有 en-US 各一行（不许只写中文），实际 %d", got)
	}
	var rulePairs []struct {
		ItemKey string
		ZH      string
		EN      string
	}
	if err := db.Table("sys_i18n AS z").
		Select("z.item_key AS item_key, z.item_value AS zh, e.item_value AS en").
		Joins("JOIN sys_i18n e ON e.item_key = z.item_key AND e.lang = 'en-US'").
		Where("z.lang = 'zh-CN' AND z.item_key IN (?)", ruleKeys).
		Scan(&rulePairs).Error; err != nil {
		t.Fatalf("查询 226 词条中英对失败: %v", err)
	}
	if len(rulePairs) != 4 {
		t.Fatalf("226 词条中英匹配应为 4 对，实际 %d 对", len(rulePairs))
	}
	for _, p := range rulePairs {
		if p.ZH == "" || p.EN == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if p.ZH == p.EN {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}
	}

	bulkKeys := []string{
		"admin.bulk.done", "admin.bulk.noun.admin", "admin.bulk.noun.datarule",
		"admin.bulk.noun.dept", "admin.bulk.noun.menu", "admin.bulk.noun.permission",
		"admin.bulk.noun.role", "admin.bulk.partial", "admin.i18nBulk.allDeleted",
		"admin.i18nBulk.allSkipped", "admin.i18nBulk.noneSelected", "admin.i18nBulk.partial",
		"admin.inventory.bulk.sourceDone", "admin.inventory.bulk.sourcePartial", "content.bulk.allDeleted",
		"content.bulk.allSkipped", "content.bulk.noneSelected", "content.bulk.partial",
		"order.bulk.allDone", "order.bulk.allSkipped", "order.bulk.couponTargetInvalid",
		"order.bulk.noneSelected", "order.bulk.noun.coupon", "order.bulk.noun.order",
		"order.bulk.noun.return", "order.bulk.partial", "order.bulk.verb.approved",
		"order.bulk.verb.cancelled", "order.bulk.verb.deleted", "order.bulk.verb.disabled",
		"order.bulk.verb.enabled", "order.bulk.verb.flowed", "order.bulk.verb.rejected",
		"page.bulk.pageAllDeleted", "page.bulk.pageAllSkipped", "page.bulk.pageMissingID",
		"page.bulk.pageNoneSelected", "page.bulk.pagePartial", "page.bulk.redirectAllDeleted",
		"page.bulk.redirectAllSkipped", "page.bulk.redirectNoneSelected", "page.bulk.redirectPartial",
		"product.bulk.attrDone", "product.bulk.attrPartial", "product.bulk.brandDone",
		"product.bulk.brandPartial", "product.bulk.categoryDone", "product.bulk.categoryPartial",
		"product.bulk.pricingAllSkip", "product.bulk.pricingApplied", "product.bulk.pricingNoChange",
		"product.bulk.pricingNoneSelected", "product.bulk.pricingPartial", "product.bulk.productDone",
		"product.bulk.productPartial", "product.bulk.tagDone", "product.bulk.tagPartial",
		"product.bulk.variantSaveNoChange", "product.bulk.variantSaveSaved", "product.bulk.variantSaveSkipped",
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", bulkKeys, "zh-CN"); got != 60 {
		t.Fatalf("283 的 60 个 key 应有 zh-CN 各一行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", bulkKeys, "en-US"); got != 60 {
		t.Fatalf("283 的 60 个 key 应有 en-US 各一行（不许只写中文），实际 %d", got)
	}
	var bulkPairs []struct {
		ItemKey string
		ZH      string
		EN      string
	}
	if err := db.Table("sys_i18n AS z").
		Select("z.item_key AS item_key, z.item_value AS zh, e.item_value AS en").
		Joins("JOIN sys_i18n e ON e.item_key = z.item_key AND e.lang = 'en-US'").
		Where("z.lang = 'zh-CN' AND z.item_key IN (?)", bulkKeys).
		Scan(&bulkPairs).Error; err != nil {
		t.Fatalf("查询 283 词条中英对失败: %v", err)
	}
	if len(bulkPairs) != 60 {
		t.Fatalf("283 词条中英匹配应为 60 对，实际 %d 对", len(bulkPairs))
	}
	for _, p := range bulkPairs {
		if p.ZH == "" || p.EN == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if p.ZH == p.EN {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}
		if !onlyStringPlaceholders(p.ZH) || !onlyStringPlaceholders(p.EN) {
			t.Fatalf("%s 只允许 %%s 占位符（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
	}

	pageHitKeys := []string{
		"admin.product_tags.list.hitHint", "admin.product_tags.hits.error",
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", pageHitKeys, "zh-CN"); got != 2 {
		t.Fatalf("298 的 2 个 key 应有 zh-CN 各一行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", pageHitKeys, "en-US"); got != 2 {
		t.Fatalf("298 的 2 个 key 应有 en-US 各一行（不许只写中文），实际 %d", got)
	}
	var pageHitPairs []struct {
		ItemKey string
		ZH      string
		EN      string
	}
	if err := db.Table("sys_i18n AS z").
		Select("z.item_key AS item_key, z.item_value AS zh, e.item_value AS en").
		Joins("JOIN sys_i18n e ON e.item_key = z.item_key AND e.lang = 'en-US'").
		Where("z.lang = 'zh-CN' AND z.item_key IN (?)", pageHitKeys).
		Scan(&pageHitPairs).Error; err != nil {
		t.Fatalf("查询 298 词条中英对失败: %v", err)
	}
	if len(pageHitPairs) != 2 {
		t.Fatalf("298 词条中英匹配应为 2 对，实际 %d 对", len(pageHitPairs))
	}
	for _, p := range pageHitPairs {

		if strings.TrimSpace(p.ZH) == "" || strings.TrimSpace(p.EN) == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if strings.TrimSpace(p.ZH) == strings.TrimSpace(p.EN) {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}

		t.Logf("298 %s：zh=%q en=%q", p.ItemKey, p.ZH, p.EN)
	}

	for _, p := range pageHitPairs {
		if p.ItemKey != "admin.product_tags.hits.error" {
			continue
		}
		if !strings.HasSuffix(p.EN, ": ") {
			t.Fatalf("hits.error en-US 应以冒号加空格结尾（模板直接拼接错误正文），实际 %q", p.EN)
		}
	}

	foundSeed := false
	for _, s := range migrations.AllSeeds() {
		if s.Version != "298-i18n-seed-product-tag-hits" {
			continue
		}
		foundSeed = true
		var done int64
		if err := db.Raw(s.ConditionSQL).Scan(&done).Error; err != nil {
			t.Fatalf("求值 298 的幂等判定失败: %v（ConditionSQL=%s）", err, s.ConditionSQL)
		}
		if done <= 0 {
			t.Fatalf("298 的幂等判定在词条落库后仍为 %d —— seed 的 ConditionSQL 由迁移器直接 db.Raw 执行、没有任何参数替换，key 必须写进 SQL 字面量；否则判定恒为 0、每次启动重跑", done)
		}
	}
	if !foundSeed {
		t.Fatal("注册台账里找不到 298-i18n-seed-product-tag-hits（新增 seed 必须注册）")
	}

	relatedKeys := []string{"ErrRelatedInvalid"}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", relatedKeys, "zh-CN"); got != 1 {
		t.Fatalf("304 的 1 个 key 应有 zh-CN 各一行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", relatedKeys, "en-US"); got != 1 {
		t.Fatalf("304 的 1 个 key 应有 en-US 各一行（不许只写中文），实际 %d", got)
	}
	var relatedPairs []struct {
		ItemKey string
		ZH      string
		EN      string
	}
	if err := db.Table("sys_i18n AS z").
		Select("z.item_key AS item_key, z.item_value AS zh, e.item_value AS en").
		Joins("JOIN sys_i18n e ON e.item_key = z.item_key AND e.lang = 'en-US'").
		Where("z.lang = 'zh-CN' AND z.item_key IN (?)", relatedKeys).
		Scan(&relatedPairs).Error; err != nil {
		t.Fatalf("查询 304 词条中英对失败: %v", err)
	}
	if len(relatedPairs) != 1 {
		t.Fatalf("304 词条中英匹配应为 1 对，实际 %d 对", len(relatedPairs))
	}
	for _, p := range relatedPairs {

		if strings.TrimSpace(p.ZH) == "" || strings.TrimSpace(p.EN) == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if strings.TrimSpace(p.ZH) == strings.TrimSpace(p.EN) {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}

		t.Logf("304 %s：zh=%q en=%q", p.ItemKey, p.ZH, p.EN)
	}

	foundRelatedSeed := false
	for _, s := range migrations.AllSeeds() {
		if s.Version != "304-i18n-seed-product-related-ids" {
			continue
		}
		foundRelatedSeed = true
		var done int64
		if err := db.Raw(s.ConditionSQL).Scan(&done).Error; err != nil {
			t.Fatalf("求值 304 的幂等判定失败: %v（ConditionSQL=%s）", err, s.ConditionSQL)
		}
		if done <= 0 {
			t.Fatalf("304 的幂等判定在词条落库后仍为 %d —— ConditionSQL 由迁移器直接 db.Raw 执行、没有任何参数替换，key 必须写进 SQL 字面量；否则判定恒为 0、每次启动重跑", done)
		}
	}
	if !foundRelatedSeed {
		t.Fatal("注册台账里找不到 304-i18n-seed-product-related-ids（新增 seed 必须注册）")
	}

	tagCrossKeys := []string{"ErrTagCrossProject"}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", tagCrossKeys, "zh-CN"); got != 1 {
		t.Fatalf("312 的 1 个 key 应有 zh-CN 各一行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key IN (?) AND lang = ?", tagCrossKeys, "en-US"); got != 1 {
		t.Fatalf("312 的 1 个 key 应有 en-US 各一行（不许只写中文），实际 %d", got)
	}
	var tagCrossPairs []struct {
		ItemKey string
		ZH      string
		EN      string
	}
	if err := db.Table("sys_i18n AS z").
		Select("z.item_key AS item_key, z.item_value AS zh, e.item_value AS en").
		Joins("JOIN sys_i18n e ON e.item_key = z.item_key AND e.lang = 'en-US'").
		Where("z.lang = 'zh-CN' AND z.item_key IN (?)", tagCrossKeys).
		Scan(&tagCrossPairs).Error; err != nil {
		t.Fatalf("查询 312 词条中英对失败: %v", err)
	}
	if len(tagCrossPairs) != 1 {
		t.Fatalf("312 词条中英匹配应为 1 对，实际 %d 对", len(tagCrossPairs))
	}
	for _, p := range tagCrossPairs {

		if strings.TrimSpace(p.ZH) == "" || strings.TrimSpace(p.EN) == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if strings.TrimSpace(p.ZH) == strings.TrimSpace(p.EN) {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}

		t.Logf("312 %s：zh=%q en=%q", p.ItemKey, p.ZH, p.EN)
	}

	foundTagCrossSeed := false
	for _, s := range migrations.AllSeeds() {
		if s.Version != "312-i18n-seed-product-tag-cross-project" {
			continue
		}
		foundTagCrossSeed = true
		var done int64
		if err := db.Raw(s.ConditionSQL).Scan(&done).Error; err != nil {
			t.Fatalf("求值 312 的幂等判定失败: %v（ConditionSQL=%s）", err, s.ConditionSQL)
		}
		if done <= 0 {
			t.Fatalf("312 的幂等判定在词条落库后仍为 %d —— ConditionSQL 由迁移器直接 db.Raw 执行、没有任何参数替换，key 必须写进 SQL 字面量；否则判定恒为 0、每次启动重跑", done)
		}
	}
	if !foundTagCrossSeed {
		t.Fatal("注册台账里找不到 312-i18n-seed-product-tag-cross-project（新增 seed 必须注册）")
	}

	const siteComponentFloor = 56
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'site.component.%' AND lang = ?", "zh-CN"); got < siteComponentFloor {
		t.Fatalf("site.component.* zh-CN 至少应有 %d 行（060 的 13 + 065 的 1 + 176 的 16 + 177 的 26），实际 %d", siteComponentFloor, got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'site.component.%' AND lang = ?", "en-US"); got < siteComponentFloor {
		t.Fatalf("site.component.* en-US 至少应有 %d 行（缺失 = 英文界面回落中文兜底），实际 %d", siteComponentFloor, got)
	}

	if got := countRows(t, db, "sys_i18n", "item_key = 'site.component.languages.label' AND lang = ?", "zh-CN"); got != 1 {
		t.Fatalf("site.component.languages.label zh-CN 应为 1 行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key = 'site.component.languages.label' AND lang = ?", "en-US"); got != 1 {
		t.Fatalf("site.component.languages.label en-US 应为 1 行，实际 %d", got)
	}

	siteKeys := []string{
		"site.component.gallery.prev", "site.component.gallery.next",
		"site.component.slider.prev", "site.component.slider.next", "site.component.slider.slide_label",
		"site.component.nav.label", "site.component.video.title",
		"site.component.countdown.days", "site.component.countdown.hours",
		"site.component.countdown.minutes", "site.component.countdown.seconds",
		"site.component.form.submit", "site.component.rating.label",
	}
	for _, key := range siteKeys {
		var zh, en string
		if err := db.Table("sys_i18n").Select("item_value").
			Where("item_key = ? AND lang = ?", key, "zh-CN").Scan(&zh).Error; err != nil {
			t.Fatalf("查询 %s zh-CN 失败: %v", key, err)
		}
		if err := db.Table("sys_i18n").Select("item_value").
			Where("item_key = ? AND lang = ?", key, "en-US").Scan(&en).Error; err != nil {
			t.Fatalf("查询 %s en-US 失败: %v", key, err)
		}
		if zh == "" || en == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", key, zh, en)
		}
		if zh == en {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", key, zh)
		}
		if !onlyStringPlaceholders(zh) || !onlyStringPlaceholders(en) {
			t.Fatalf("%s 只允许 %%s 占位符（zh=%q en=%q）", key, zh, en)
		}
	}
	var ratingZH, ratingEN string
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "site.component.rating.label", "zh-CN").Scan(&ratingZH).Error
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "site.component.rating.label", "en-US").Scan(&ratingEN).Error
	if ratingZH != "评分 %s / %s" || ratingEN != "Rated %s out of %s" {
		t.Fatalf("rating.label 词条不符：zh=%q en=%q", ratingZH, ratingEN)
	}

	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'shell.%' AND lang = ?", "zh-CN"); got != 33 {
		t.Fatalf("shell.* zh-CN 应为 33 行（059 的 25 + shell.nav.* 2 + shell.htmx.* 3 + shell.login.dev_login* 2 + 277 的 1），实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'shell.%' AND lang = ?", "en-US"); got != 33 {
		t.Fatalf("shell.* en-US 应为 33 行（同 zh-CN），实际 %d", got)
	}

	var bulkZH, bulkEN string
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "shell.err.bulkIdsTooMany", "zh-CN").Scan(&bulkZH).Error
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "shell.err.bulkIdsTooMany", "en-US").Scan(&bulkEN).Error
	if bulkZH == "" || bulkEN == "" {
		t.Fatalf("shell.err.bulkIdsTooMany 中英文案不得为空（zh=%q en=%q）", bulkZH, bulkEN)
	}
	if bulkZH == bulkEN {
		t.Fatalf("shell.err.bulkIdsTooMany 中英文案相同（%q），疑似未翻译", bulkZH)
	}
	if !onlyStringPlaceholders(bulkZH) || !onlyStringPlaceholders(bulkEN) {
		t.Fatalf("shell.err.bulkIdsTooMany 只允许 %%s 占位符（zh=%q en=%q）", bulkZH, bulkEN)
	}
	if strings.Count(bulkZH, "%s") != 2 || strings.Count(bulkEN, "%s") != 2 {
		t.Fatalf("shell.err.bulkIdsTooMany 应各含两个 %%s（上限、本次条数），实际 zh=%q en=%q", bulkZH, bulkEN)
	}
	var shellValue string
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "shell.brand", "en-US").Scan(&shellValue).Error; err != nil {
		t.Fatalf("查询 shell.brand en-US 失败: %v", err)
	}
	if shellValue != "Admin Console" {
		t.Fatalf("shell.brand en-US = %q，want Admin Console", shellValue)
	}
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "shell.pagination.info", "zh-CN").Scan(&shellValue).Error; err != nil {
		t.Fatalf("查询 shell.pagination.info zh-CN 失败: %v", err)
	}
	if !onlyStringPlaceholders(shellValue) {
		t.Fatalf("shell.pagination.info zh-CN 只允许 %%s 占位符，实际 %q", shellValue)
	}

	if got := countRows(t, db, "sys_i18n", "item_key = ?", "ErrAdminNotFound"); got != 2 {
		t.Fatalf("ErrAdminNotFound 应有 zh-CN/en-US 两行，实际 %d", got)
	}
	var value string
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "ErrAdminNotFound", "zh-CN").Scan(&value).Error; err != nil {
		t.Fatalf("查询 ErrAdminNotFound zh-CN 失败: %v", err)
	}
	if value != "管理员不存在" {
		t.Fatalf("ErrAdminNotFound zh-CN = %q，want 管理员不存在", value)
	}

	if got := countRows(t, db, "sys_i18n", "item_key = ?", "ErrMenuDepthExceeded"); got != 2 {
		t.Fatalf("ErrMenuDepthExceeded 应有 zh-CN/en-US 两行（430 已补英文），实际 %d", got)
	}
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "ErrMenuDepthExceeded", "en-US").Scan(&value).Error; err != nil {
		t.Fatalf("查询 ErrMenuDepthExceeded en-US 失败: %v", err)
	}
	if strings.TrimSpace(value) == "" || value == "菜单层级超过上限（3 级）" {
		t.Fatalf("ErrMenuDepthExceeded en-US = %q，英文译文缺失或是中文的复制", value)
	}

	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "MsgPageCreated", "zh-CN").Scan(&value).Error; err != nil {
		t.Fatalf("查询 MsgPageCreated zh-CN 失败: %v", err)
	}
	if value != "页面创建成功" {
		t.Fatalf("MsgPageCreated zh-CN = %q，want 页面创建成功", value)
	}

	categories := map[string]string{"ErrSystemError": "error", "uiOpSuccess": "msg", "MsgPageCreated": "ui"}
	for key, want := range categories {
		var got string
		if err := db.Table("sys_i18n").Select("category").
			Where("item_key = ? AND lang = ?", key, "zh-CN").Scan(&got).Error; err != nil {
			t.Fatalf("查询 %s category 失败: %v", key, err)
		}
		if got != want {
			t.Fatalf("%s category = %q，want %q", key, got, want)
		}
	}

	var remark string
	if err := db.Table("sys_i18n").Select("remark").
		Where("item_key = ? AND lang = ?", "MsgPageCreated", "zh-CN").Scan(&remark).Error; err != nil {
		t.Fatalf("查询 MsgPageCreated remark 失败: %v", err)
	}
	if remark != "internal/module/page/enums/page_enums.go" {
		t.Fatalf("MsgPageCreated remark = %q，want 来源文件路径", remark)
	}
}

func TestI18nEnumsSeedResponseTranslation(t *testing.T) {
	dbName := "go_test_i18n_" + randomSuffix()
	admin, err := gorm.Open(postgres.Open(i18nTestDSN("postgres")), &gorm.Config{})
	if err != nil {
		support.FailIfRequiredPG(t, err)
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}
	if err := admin.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Skipf("跳过：创建临时测试库失败: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + dbName)
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := newI18nTestConfig(dbName)
	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	t.Cleanup(func() {
		_ = i18n.Close()
		_ = database.Close()
	})

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("结构迁移失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("词条 seed 失败: %v", err)
	}
	if err := i18n.Init(cfg); err != nil {
		t.Fatalf("初始化 i18n 缓存失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/ok", func(c *gin.Context) { response.Success(c, gin.H{"id": 1}) })
	engine.GET("/notfound", func(c *gin.Context) { response.ErrorWithMessage(c, http.StatusNotFound, enums.ErrNotFound) })
	engine.GET("/admin", func(c *gin.Context) { response.ErrorWithMessage(c, http.StatusNotFound, adminenums.ErrAdminNotFound) })
	engine.GET("/page", func(c *gin.Context) { response.SuccessWithMessage(c, pageenums.MsgPageCreated, nil) })
	engine.GET("/locked", func(c *gin.Context) {
		response.ErrorWithMessage(c, http.StatusLocked, adminenums.ErrAccountLocked+"|5m0s")
	})
	engine.GET("/detail", func(c *gin.Context) {
		response.ErrorWithMessage(c, http.StatusBadRequest, adminenums.MsgBadRequest+": boom")
	})

	fetch := func(path, acceptLang string) response.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if acceptLang != "" {
			req.Header.Set("Accept-Language", acceptLang)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		var resp response.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析 %s 响应失败: %v（body=%s）", path, err, w.Body.String())
		}
		return resp
	}

	cases := []struct {
		name string
		path string
		lang string
		key  string
		want string
	}{
		{"Success 通用消息", "/ok", "zh-CN", "uiOpSuccess", "操作成功"},
		{"纯 key 形态", "/notfound", "zh-CN", "ErrNotFound", "请求资源不存在"},
		{"模块错误 key", "/admin", "zh-CN", "ErrAdminNotFound", "管理员不存在"},
		{"模块成功 key", "/page", "zh-CN", "MsgPageCreated", "页面创建成功"},
		{"key|param 形态", "/locked", "zh-CN", "ErrAccountLocked", "账号已被锁定，请 5m0s 后重试"},
		{"key: detail 形态", "/detail", "zh-CN", "sysErrBadParam", "请求参数错误: boom"},
		{"Accept-Language 切英文", "/admin", "en-US", "ErrAdminNotFound", "Admin not found"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := fetch(tc.path, tc.lang)
			if resp.Message == tc.key {
				t.Fatalf("message 仍是裸 key %q（未命中 i18n 缓存）", tc.key)
			}
			if resp.Message != tc.want {
				t.Fatalf("message = %q, want %q", resp.Message, tc.want)
			}
		})
	}

	resp := fetch("/notfound", "ja-JP")
	if resp.Message != "请求资源不存在" {
		t.Fatalf("不支持语言应回退默认语言，实际 %q", resp.Message)
	}
}
