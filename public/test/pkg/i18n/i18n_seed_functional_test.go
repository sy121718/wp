package i18n_test

// i18n_seed_functional_test.go — i18n 数据层（P0：表结构 + 词条 seed）验证。
//
// 覆盖：
//  1) 055/056/057 表结构迁移 + 058 enums 词条 seed 在真实 PostgreSQL 上执行且可重复执行（幂等）；
//  2) 干净 schema 上的精确行数（zh-CN 195 / en-US 79）、分类推导与来源备注；
//  3) 接口级：种子词条经 pkg/i18n 缓存命中后，pkg/response 的 JSON 响应返回文案而不是裸 key
//     （纯 key / key|param / key: detail 三种形态 + Accept-Language 语言切换）。
//
// PG 不可用时 t.Skip（与其他功能测试一致）。

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// i18nTestDSN 拼装 libpq 风格 DSN（与 support.pgDSN 一致）。
func i18nTestDSN(dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		support.DefaultPGHost, support.DefaultPGUser, support.DefaultPGPassword, dbname, support.DefaultPGPort)
}

// newI18nTestConfig 构造指向指定库的测试配置。
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

// countRows 统计满足条件的行数。
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

// columnCount 统计某表上存在的一批列（用于断言新增列真实落库）。
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

// TestI18nEnumsSeedSchemaAndIdempotency 在干净隔离 schema 上验证注册的迁移 + seed：
// 结构落库、精确行数、分类推导、en-US 不伪造，且重复执行幂等。
func TestI18nEnumsSeedSchemaAndIdempotency(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}

	// 全量迁移 + seed 连跑两轮：第二轮应全部命中幂等守卫，不报错、不重复插入。
	for round := 1; round <= 2; round++ {
		if err := migrations.Run(db); err != nil {
			t.Fatalf("第 %d 轮结构迁移失败: %v", round, err)
		}
		if err := migrations.RunSeeds(db); err != nil {
			t.Fatalf("第 %d 轮 seed 失败: %v", round, err)
		}
	}

	// 1) sys_i18n 补 category / remark（055）
	if got := columnCount(t, db, "sys_i18n", "category", "remark"); got != 2 {
		t.Fatalf("sys_i18n 应存在 category/remark 两列，实际 %d", got)
	}
	// 2) sys_i18n_revision 单行表（056）
	if got := countRows(t, db, "sys_i18n_revision", ""); got != 1 {
		t.Fatalf("sys_i18n_revision 应为单行，实际 %d 行", got)
	}
	// 3) sys_menus.title_key 列（057）
	if got := columnCount(t, db, "sys_menus", "title_key"); got != 1 {
		t.Fatalf("sys_menus 应存在 title_key 列，实际 %d", got)
	}

	// 4) 干净迁移库的精确总量。228 角色权限页新增 15 对词条，原账本漏记了这批；
	// 297 块删除保护的引用类别词条新增 7 对（MsgBlockUsage*，中英各 7 行）；
	// 298（商品标签页命中商品展开区）又新增 2 对 —— 两份账本同批并入。
	// 业务词条仍在下文按 key 和语言逐项校验，总量不能替代语义检查。
	for lang, want := range map[string]int64{"zh-CN": 3778, "en-US": 3663} {
		got := countRows(t, db, "sys_i18n", "lang = ?", lang)
		if got != want {
			t.Fatalf("sys_i18n %s 词条数量：want=%d got=%d（新增 seed 时同步核对各语言）", lang, want, got)
		}
		// -v 时把实际行数打出来：新增 seed 后核对账本时不必只信断言。
		t.Logf("sys_i18n %s = %d 行（账本 want=%d）", lang, got, want)
	}

	// 4-C) 本批（222）的后台访问统计维度榜词条：18 个 key 中英成对，且取值不同。
	//
	// 总数 ledger 只保证「行数对得上」——漏一条 en-US、同时多写一条别的 key 也能凑数，
	// 所以这里按本批 key 逐条对账（含「英文不是复制中文」这一项，与 4-B 同形）。
	// 这批词条的特殊性在于：模板兜底文案本身就是中文，漏了 en-US 不会有任何报错，
	// 只会让英文界面显示中文 —— 那种缺陷在测试里不钉住，就只能等用户看见。
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

	// 4-D) 数据规则配置校验词条（226）：4 个 key，中英成对且取值不同。
	// 域白名单收口后，规则的字段/操作符越界不再静默落库，而是报出究竟哪一项越界 ——
	// 缺了 en-US 只会让英文界面显示裸 key，与其它批次同样的坑，所以按 key 逐条对账。
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

	// 4-E) 批量操作结论文案词条（283）：60 个 key，中英成对、取值不同、且只允许 %s 占位符。
	//
	// 这一批的特殊性：写侧与读侧**共用同一份取词**（词条缺失时两侧同时回退中文原文），
	// 于是漏一条 en-US 不会有任何报错 —— 英文界面上显示中文而已。所以按 key 逐条对账，
	// 并且比一般批次多钉一条「占位符只允许 %s」：计数在 Go 侧 strconv.Itoa 之后再填，
	// 词条里混进 %d 会让 Sprintf 把参数渲染成 %!d(string=3)，而这条路直接给运营看。
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
		if !i18n.HasStringPlaceholdersOnly(p.ZH) || !i18n.HasStringPlaceholdersOnly(p.EN) {
			t.Fatalf("%s 只允许 %%s 占位符（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
	}

	// 4-F) 商品标签页「命中商品」展开区词条（298 / 审计 PERF-02）：2 个 key 中英成对、非空、取值不同。
	//
	// 这批词的坑与 222/226 同形：模板兜底就是中文，漏了 en-US 既不报错也不至于裸 key，
	// 只是英文界面上显示中文。所以按本批 key 逐条对账，而不是只信总数 ledger。
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
		// 英文不算「空」但只有空白同样不可用：0 个可见字符的译文与缺失等价。
		if strings.TrimSpace(p.ZH) == "" || strings.TrimSpace(p.EN) == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if strings.TrimSpace(p.ZH) == strings.TrimSpace(p.EN) {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}
		// -v 时打出实际取值：复核本批词条不必再手动查库。
		t.Logf("298 %s：zh=%q en=%q", p.ItemKey, p.ZH, p.EN)
	}
	// hits.error 的英文以冒号 + 一个空格结尾：模板把它与错误正文直接拼接，
	// 少了这个空格会拼成 "products:Tag not found"。这条在这里钉死。
	for _, p := range pageHitPairs {
		if p.ItemKey != "admin.product_tags.hits.error" {
			continue
		}
		if !strings.HasSuffix(p.EN, ": ") {
			t.Fatalf("hits.error en-US 应以冒号加空格结尾（模板直接拼接错误正文），实际 %q", p.EN)
		}
	}

	// 4-F2) 本条 seed 的**幂等判定**必须真的成立：用注册台账里的同一条 ConditionSQL 求值，
	// 而不是另写一句近似查询 —— 否则测的是「我以为的判据」，不是迁移器真正执行的那一句。
	// 判定恒为 0（例如把 key 写成 ? 占位符）时 seed 不报错、行数也对，只是每次启动重跑。
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

	// 4-B) 访客面组件词条（060）：13 个 key，zh-CN/en-US 各一行；中英必须都有（不许缺翻译）。
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'site.component.%' AND lang = ?", "zh-CN"); got != 56 {
		t.Fatalf("site.component.* zh-CN 应为 56 行（060 的 13 + 065 的 1 + 176 的 16 + 177 的 26），实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'site.component.%' AND lang = ?", "en-US"); got != 56 {
		t.Fatalf("site.component.* en-US 应为 56 行（060 的 13 + 065 的 1 + 176 的 16 + 177 的 26），实际 %d", got)
	}
	// 065 语言切换器容器标签：中英各一行且取值不同。
	if got := countRows(t, db, "sys_i18n", "item_key = 'site.component.languages.label' AND lang = ?", "zh-CN"); got != 1 {
		t.Fatalf("site.component.languages.label zh-CN 应为 1 行，实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key = 'site.component.languages.label' AND lang = ?", "en-US"); got != 1 {
		t.Fatalf("site.component.languages.label en-US 应为 1 行，实际 %d", got)
	}
	// 逐条断言：中英取值不同（文案确实被翻译，不是复制中文）+ 占位符仅 %s。
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
		if !i18n.HasStringPlaceholdersOnly(zh) || !i18n.HasStringPlaceholdersOnly(en) {
			t.Fatalf("%s 只允许 %%s 占位符（zh=%q en=%q）", key, zh, en)
		}
	}
	var ratingZH, ratingEN string
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "site.component.rating.label", "zh-CN").Scan(&ratingZH).Error
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "site.component.rating.label", "en-US").Scan(&ratingEN).Error
	if ratingZH != "评分 %s / %s" || ratingEN != "Rated %s out of %s" {
		t.Fatalf("rating.label 词条不符：zh=%q en=%q", ratingZH, ratingEN)
	}

	// 4-A) 后台外壳词条：059 的 25 个 key + UI-001 的 shell.nav.open/close（窄屏抽屉按钮的
	// 无障碍标签）+ UI-011 的 shell.htmx.error/network/timeout（HTMX 失败的三种兜底文案）
	// + shell.login.dev_login / shell.login.dev_login_hint（开发态登录入口的标签与提示，
	// 436d20b 加入；这两个词条此前漏更新了这里的计数，总数断言修好后一并订正）
	// + 277 的 shell.err.bulkIdsTooMany（批量 id 超限的受控提示，壳层自己的错误文案 ——
	// shell.BulkIDsFacingText 按当前语言取它；这是 shell 命名空间里第一条 err.* 词条）。
	// zh-CN/en-US 各一行；分页文案占位符仅 %s。
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'shell.%' AND lang = ?", "zh-CN"); got != 33 {
		t.Fatalf("shell.* zh-CN 应为 33 行（059 的 25 + shell.nav.* 2 + shell.htmx.* 3 + shell.login.dev_login* 2 + 277 的 1），实际 %d", got)
	}
	if got := countRows(t, db, "sys_i18n", "item_key LIKE 'shell.%' AND lang = ?", "en-US"); got != 33 {
		t.Fatalf("shell.* en-US 应为 33 行（同 zh-CN），实际 %d", got)
	}
	// 277 的批量 id 超限词条：中英各一行、取值不同、只允许 %s 且**恰好两个**（上限、本次条数）。
	// 只断言「有这一行」不够 —— 少一个占位符时 Sprintf 会输出 %!s(MISSING)，
	// 而那正是「文案与错误里的数字脱钩」的隐蔽形态。
	var bulkZH, bulkEN string
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "shell.err.bulkIdsTooMany", "zh-CN").Scan(&bulkZH).Error
	_ = db.Table("sys_i18n").Select("item_value").Where("item_key = ? AND lang = ?", "shell.err.bulkIdsTooMany", "en-US").Scan(&bulkEN).Error
	if bulkZH == "" || bulkEN == "" {
		t.Fatalf("shell.err.bulkIdsTooMany 中英文案不得为空（zh=%q en=%q）", bulkZH, bulkEN)
	}
	if bulkZH == bulkEN {
		t.Fatalf("shell.err.bulkIdsTooMany 中英文案相同（%q），疑似未翻译", bulkZH)
	}
	if !i18n.HasStringPlaceholdersOnly(bulkZH) || !i18n.HasStringPlaceholdersOnly(bulkEN) {
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
	if !i18n.HasStringPlaceholdersOnly(shellValue) {
		t.Fatalf("shell.pagination.info zh-CN 只允许 %%s 占位符，实际 %q", shellValue)
	}

	// 5) 命中 a2 的 key：zh-CN + en-US 各一行，值取自 a2
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

	// 6) 新增 key（a2 无）：只有 zh-CN，不得伪造 en-US
	if got := countRows(t, db, "sys_i18n", "item_key = ?", "ErrMenuDepthExceeded"); got != 1 {
		t.Fatalf("ErrMenuDepthExceeded 应只有 zh-CN 一行（en-US 待翻译），实际 %d", got)
	}
	// 7) 新 key 化的原中文直值常量：值 = 常量名（key），文案进 zh-CN
	if err := db.Table("sys_i18n").Select("item_value").
		Where("item_key = ? AND lang = ?", "MsgPageCreated", "zh-CN").Scan(&value).Error; err != nil {
		t.Fatalf("查询 MsgPageCreated zh-CN 失败: %v", err)
	}
	if value != "页面创建成功" {
		t.Fatalf("MsgPageCreated zh-CN = %q，want 页面创建成功", value)
	}

	// 8) category 推导：Err* → error；msg_* → msg；其余 → ui
	categories := map[string]string{"ErrSystemError": "error", "msg_operation_success": "msg", "MsgPageCreated": "ui"}
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

	// 9) remark 记录词条来源文件（便于回溯）
	var remark string
	if err := db.Table("sys_i18n").Select("remark").
		Where("item_key = ? AND lang = ?", "MsgPageCreated", "zh-CN").Scan(&remark).Error; err != nil {
		t.Fatalf("查询 MsgPageCreated remark 失败: %v", err)
	}
	if remark != "internal/module/page/enums/page_enums.go" {
		t.Fatalf("MsgPageCreated remark = %q，want 来源文件路径", remark)
	}
}

// TestI18nEnumsSeedResponseTranslation 接口级验证：在独立临时库上跑全量迁移 + seed，
// 经 pkg/i18n 缓存命中后，pkg/response 的 JSON 响应返回文案而不是裸 key。
//
// 用独立临时库（而非共享 wp_test）是因为 pkg/i18n 的缓存加载走全局 database 组件，
// 而 wp_test 的历史残留表结构不完整；建库/删库逻辑与 pkg/response 的 MySQL 用例同构。
func TestI18nEnumsSeedResponseTranslation(t *testing.T) {
	dbName := "go_test_i18n_" + randomSuffix()
	admin, err := gorm.Open(postgres.Open(i18nTestDSN("postgres")), &gorm.Config{})
	if err != nil {
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
		{"Success 通用消息", "/ok", "zh-CN", "msg_operation_success", "操作成功"},
		{"纯 key 形态", "/notfound", "zh-CN", "ErrNotFound", "请求资源不存在"},
		{"模块错误 key", "/admin", "zh-CN", "ErrAdminNotFound", "管理员不存在"},
		{"模块成功 key", "/page", "zh-CN", "MsgPageCreated", "页面创建成功"},
		{"key|param 形态", "/locked", "zh-CN", "ErrAccountLocked", "账号已被锁定，请 5m0s 后重试"},
		{"key: detail 形态", "/detail", "zh-CN", "ErrInvalidParams", "请求参数错误: boom"},
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

	// 不支持的语言回退默认语言（zh-CN），不应返回空串或裸 key。
	resp := fetch("/notfound", "ja-JP")
	if resp.Message != "请求资源不存在" {
		t.Fatalf("不支持语言应回退默认语言，实际 %q", resp.Message)
	}
}
