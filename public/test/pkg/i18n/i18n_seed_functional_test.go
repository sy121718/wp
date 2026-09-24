package i18n_test

// i18n_seed_functional_test.go — i18n 数据层（P0：表结构 + 词条 seed）验证。
//
// 覆盖：
//  1) 055/056/057 表结构迁移 + 058 enums 词条 seed 在真实 PostgreSQL 上执行且可重复执行（幂等）；
//  2) 干净数据库上的双语数量下限与全量 key 对账、分类推导与来源备注；
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
// 结构落库、双语键完整性、分类推导、en-US 不伪造，且重复执行幂等。
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

	// 4) 历史种子增量记录（实际完整性由下方双语下限与差集断言守护）。
	// 228 角色权限页新增 15 对词条，原账本漏记了这批；
	// 297 块删除保护的引用类别词条新增 7 对（MsgBlockUsage*，中英各 7 行）；
	// 298（商品标签页命中商品展开区）又新增 2 对 —— 两份账本同批并入；
	// 304（商品「相关商品」引用校验）新增 1 对（ErrRelatedInvalid，中英各一行）—— 同批并入；
	// 312（商品标签跨工程引用拒绝）新增 1 对（ErrTagCrossProject，中英各一行）—— 同批并入。
	//
	// 2026-09「失败出口收口」多批次并行落地后统一重算（此前停在 3780/3665）：
	//   313~317 / 399~402（往批：文章详情模板 props、商品编辑与只读文案、商品预览覆盖、
	//     库存流水空态、列表空态与客户端筛选空态）
	//   403 项目域页面出口（8 对）/ 404 页面域（2 对）/ 405 block 补 en-US（0 对，仅 1 行 en）
	//   406 order（2 对）/ 407 商品详情模板依赖缺失（1 对；其 loadFailed* 与 MsgInternalError
	//     的 en-US 与 405/406 重叠，被 ON CONFLICT 跳过，**不计入本迁移的贡献**）
	//   408 三域页面出口（8 对）/ 409 运行时片段出口（5 对）/ 410 邮件访客面（5 对）
	//
	// 2026-09「孤儿词条退役」（419）：
	//   删掉 7 对（14 行）—— admin.product_{categories,brands}.seo.unset（改用硬编码「—」）、
	//   admin.inventory.pagination.{info,more,end}（换成真源分页条，走 shell.pagination.*）、
	//   admin.mail.campaign.page_{prefix,suffix}（换成真分页条）；
	//   同批把 190 / 230 / 412 三处 seed 的 SQL 与幂等条件一起收口（见 419 头部与 register 注释）。
	//   **本批实测**（干净 schema 跑全量迁移 + 两轮 seed，见交付报告）：这 7 个 key 各为 0 行；
	//   190 / 230 / 412 / 419 四条幂等条件全部命中（=1，即「已满足 → 跳过」，不会每轮重跑）；
	//   而各语言总量**仍是 3914 / 3802** —— 419 的 -7/语言被同期并行在途批次的等量新增抵消。
	//   故账本数字保持 3914 / 3802 不变：**账本只认实测，不认推算**（本批曾按「3914-7」推算成
	//   3907/3795，实测立刻打回 —— 这也是「计数不是判据」的一个实例）。
	// 429「facing key 缺词条」：4 个 key（ErrInvalidRange / ErrInvalidParent / ErrInvalidSlot /
	//   ErrSlotPageMiss）—— 它们早已在各域白名单里、却从未登记词条，命中时取词只能按 fallback
	//   回落成裸 key。补 zh-CN + en-US 各 4 行。
	// 430「enums 常量缺词条」：判据是**读值不读名**——同一包内 key 形态
	//   （ErrOrderNotFound="order.err.orderNotFound"）与中文常量形态（ErrInternal="操作失败…"）
	//   并存，只能按值筛。用 AST 扫 internal/module/*/enums/*.go 的 728 个常量后，剔掉 webhook 的
	//   4 个事件类型标识（order.paid / order.refunded / product.updated / content.published 是
	//   EventType 常量、点分二段、不进 err|msg 命名空间、不进取词链，白写只会多 8 行死词条），
	//   再与库对账：两类都有 361 / 只有 zh-CN 99 / 完全没有 119。
	//   补「完全没有」的 119 个 key（zh-CN + en-US 各 119 行，category 取模块名）与
	//   「只有 zh-CN」的 99 个 key 的 en-US 99 行（category / http_code 沿用该 key 既有 zh-CN 行
	//   —— 同一 key 两行必须一致，loader 装载时 http_code 取首个非零值），共 337 行。
	//   **本批实测**：zh-CN 3918→4037（+119）、en-US 3806→4024（+218），增量与预期一致但不按推算入账。
	// 总量只在「新增 seed 时同步核对」这一层起作用；业务词条仍在下文按 key 和语言逐项校验，
	// 总量不能替代语义检查。下限拦截两种语言同时漏词条、集合对账却相等的空转；
	// 上限不钉死，让后续成对新增词条不必反复维护全仓总数账本。
	const minEnglishKeys = 4240 // 干净库执行至 442 后的实测下限。
	zhCount := countRows(t, db, "sys_i18n", "lang = ?", "zh-CN")
	enCount := countRows(t, db, "sys_i18n", "lang = ?", "en-US")
	t.Logf("sys_i18n 干净库词条：zh-CN=%d en-US=%d", zhCount, enCount)
	if enCount < minEnglishKeys || zhCount < minEnglishKeys+13 {
		t.Fatalf("sys_i18n 中英词条低于已验证基线：zh-CN=%d en-US=%d，基线至少 %d/%d", zhCount, enCount, minEnglishKeys+13, minEnglishKeys)
	}

	// 058 留下的 13 个 dashboard 词条只有中文。差集必须逐个精确匹配这些历史例外：
	// 少了例外意味着 seed 漏写，两种语言同时漏掉同一例外也会被此检查捕获。
	historicalZHOnly := map[string]bool{
		"MsgAdminGenericFailed": true, "MsgAdministratorsTitle": true,
		"MsgBlocksTitle": true, "MsgDatarulesTitle": true,
		"MsgDepartmentsTitle": true, "MsgMenusTitle": true,
		"MsgNavigationsTitle": true, "MsgPermissionsTitle": true,
		"MsgRolesTitle": true, "MsgSiteSettingsSaved": true,
		"MsgSiteSettingsTitle": true, "MsgThemeSettingsTitle": true,
		"MsgThemesTitle": true,
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
	unexpected := make([]string, 0)
	for _, diff := range languageDiff {
		if diff.Side != "zh-only" || !historicalZHOnly[diff.ItemKey] {
			unexpected = append(unexpected, diff.Side+":"+diff.ItemKey)
			continue
		}
		delete(historicalZHOnly, diff.ItemKey)
	}
	if len(unexpected) > 0 || len(historicalZHOnly) > 0 || zhCount-enCount != 13 {
		t.Fatalf("sys_i18n 中英键未对齐：异常差异=%v，缺失历史中文专有键=%v，zh-CN=%d en-US=%d", unexpected, historicalZHOnly, zhCount, enCount)
	}
	// 钉住最近的新增双语 key，防止未来批次补量掩盖旧批次两语言同时漏写。
	for _, key := range []string{
		"admin.mail.marketing.contacts.empty.initial.title",
		"admin.mail.marketing.contacts.empty.initial",
		"admin.inventory.moves.title",
		"admin.media.bulk.delete",
	} {
		if got := countRows(t, db, "sys_i18n", "item_key = ? AND lang IN ?", key, []string{"zh-CN", "en-US"}); got != 2 {
			t.Fatalf("%s 应有完整中英词条，实际 %d 行", key, got)
		}
	}
	// 检查最新三批确实进入 AllSeeds，且两轮执行后的真实幂等守卫成立。
	latestSeeds := map[string]bool{
		"440-i18n-product-list-help":       false,
		"441-i18n-mail-empty-states":       false,
		"442-i18n-inventory-moves-heading": false,
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

	// 4-G) 商品「相关商品」引用校验词条（304 / 审计 DB-03 / PROD-01）：1 个 key 中英成对、
	// 非空、取值不同。
	//
	// 这批词的坑与 222/226/298 同形：ErrRelatedInvalid 的中文兜底就写在 productErrFallbacks 里，
	// 漏了 en-US 既不报错、也不至于裸 key，只是英文界面上显示中文。所以按本批 key 逐条对账，
	// 而不是只信总数 ledger。
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
		// 英文不算「空」但只有空白同样不可用（与 298 判据一致）。
		if strings.TrimSpace(p.ZH) == "" || strings.TrimSpace(p.EN) == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if strings.TrimSpace(p.ZH) == strings.TrimSpace(p.EN) {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}
		// -v 时打出实际取值：复核本批词条不必再手动查库。
		t.Logf("304 %s：zh=%q en=%q", p.ItemKey, p.ZH, p.EN)
	}
	// 幂等判定同 4-F2：取注册台账里那一条 ConditionSQL 求值（不是另写一句近似查询）——
	// key 写成 ? 占位符时判定恒为 0，seed 不报错、行数也对，只是每次启动重跑。
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

	// 4-H) 商品标签跨工程引用拒绝词条（312 / 审计 DB-03 §2.4 / PROD-02）：1 个 key 中英成对、
	// 非空、取值不同。
	//
	// 这批词的坑与 222/226/298/304 同形：标签跨工程引用的中文兜底就写在
	// productErrFallbacks 里，漏了 en-US 既不报错、也不至于裸 key，只是英文界面上显示中文。
	// 所以按本批 key 逐条对账，而不是只信总数 ledger。
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
		// 英文不算「空」但只有空白同样不可用（与 298/304 判据一致）。
		if strings.TrimSpace(p.ZH) == "" || strings.TrimSpace(p.EN) == "" {
			t.Fatalf("%s 中英文案不得为空（zh=%q en=%q）", p.ItemKey, p.ZH, p.EN)
		}
		if strings.TrimSpace(p.ZH) == strings.TrimSpace(p.EN) {
			t.Fatalf("%s 中英文案相同（%q），疑似未翻译", p.ItemKey, p.ZH)
		}
		// -v 时打出实际取值：复核本批词条不必再手动查库。
		t.Logf("312 %s：zh=%q en=%q", p.ItemKey, p.ZH, p.EN)
	}
	// 幂等判定同 4-G：取注册台账里那一条 ConditionSQL 求值（不是另写一句近似查询）——
	// key 写成 ? 占位符时判定恒为 0，seed 不报错、行数也对，只是每次启动重跑。
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

	// 6) 新增 key（a2 无）：058 当时只写 zh-CN、不伪造 en-US（那时确实没有英文译文）。
	//    2026-09 迁移 430 按「值即 key、库里只有 zh-CN」的口径复核时补齐了它的 en-US ——
	//    该断言随之演进：原意（**不得拿中文顶替英文**）原样保留，判断力反而更强
	//    （同时钉住「中英都有」与「英文不是中文的复制」）。
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
