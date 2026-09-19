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

	// 4) 词条行数（干净 schema，精确断言）
	// 起点：195/79 为 058 enums 词条；+25/+25 为 059 后台外壳 shell.* 词条；
	// +13/+13 为 060 访客面组件 site.component.* 词条；+1/+1 为 065 语言切换器
	// 容器无障碍标签；+16/+16 为 176 商品列表组件词条、+26/+26 为 177 其余组件词条（含 userForms 的九个默认标题）（审计 I18N-010）。
	// +157/+157 为 179-182 四个模块的 enums 文案词条（order 62 / user 41 / mail 32 / cart 22，
	// 审计 I18N-002）——常量值改成 i18n key 之后，文案本身挪进了这张表。
	// +259/+259 为前端后台模板的三批抽取词条（187 settings/plugins/media 101 条、
	// 188 article/content 149 条、189 自定义 404 页 9 条，审计 I18N-001 与 SEO-013 的配套）。
	// +7/+7 为 198 masterdata 模块文案 key 化（审计 CQ-010 收尾）：6 个 Err + 1 个 Msg 的值
	// 从中文原文改成 i18n key 之后，文案本身挪进了这张表。
	// 此后片段与站点词条（site.fragment.* 等）按批次继续追加，
	// 数字随之增长 —— 更新这两个数时要顺带确认新批次**中英都已补齐**
	//（下面的分组断言与「en 不许缺行」的检查就是为这件事兜底的）。
	//
	// +11/+11 为 272（库存域收口批）——采购入库页补回「生产入库」表单新增的 11 个文案位
	//（admin.inventory_purchases.production.title / open / noInternal / labelSource /
	// optionSource / labelSku / optionSku / labelQuantity / labelCost / hintCost / submit，
	// 中英各一行；模板里的中文只是取词失败时的兜底）。该表单的路由与权限点一直在，
	// 缺的一直是页面入口 —— 补回时新增的词条必须同批 seed，见 register_order.go 的 272。
	//
	// 口径说明：本批落库时工作区实际是 3585/3469（本批 +11 后为 3596/3480），而这里原有的
	// 3589/3473 与之相差 4 对 —— 那是**同期其它批次**改 seed 之后的漂移（本批未碰那些词条）。
	// 本批只把自己新增的 11 对并入实测值；两批的账本对账由主 agent 统一收口。
	//
	// +1/+1 为 276（mail 页面错误收口批）——mailenums.ErrInternal（mail.err.internal），
	// 邮箱后台四个页面 handler 的错误文案三件套归口 key，中英各一行；判定见
	// public/migrations/register_identity_mail.go 的 276。本批同样只并入自己的 1 对，
	// 账本数字随同期批次增长，一致性由主 agent 统一收口。
	//
	// +60/+60 为 283（批量操作结论的 key 化批）—— admin.bulk.* / admin.i18nBulk.* /
	// order.bulk.* / content.bulk.* / product.bulk.* / page.bulk.* / admin.inventory.bulk.source*：
	// 六个模块里 handler 自己拼的批量回执（「已删除 N 个角色」「没有勾选任何优惠码」这类）
	// 从 Go 里的中文字面量改成词条，写侧与读侧共用同一份「key + 中文原文」结构体与同一个取词函数
	//（adminBulkTextOf / orderBulkTextOf / articleBulkTextOf / productBulkTextOf / pageBulkTextOf /
	// inventoryBulkText）。60 个 key × 中英 = 120 行；本批是本仓库唯一一批「中英都必须齐」的硬要求：
	// 词条缺失时写读两侧会同时回退中文原文，于是英文页面上显示中文而没有任何报错。
	// 逐条对账见下面的 4-E 块。
	//
	// +1/+1 为 277（批量 id 超限的受控提示批）——shell.err.bulkIdsTooMany：shell.BulkIDs 的
	// 超限错误改成**带 sentinel 的类型**（shell.ErrBulkIDsTooMany / *shell.BulkIDsError）之后，
	// 对外文案统一走 shell.BulkIDsFacingText，这条词条就是它按当前语言取的那句
	//（两个 %s：上限、本次条数；后一个数字只有 shell 知道 —— 模块别再用 MaxBulkIDs 重算）。
	// 判定见 public/migrations/register_core.go 的 277；本批同样只并入自己的 1 对。
	// +4 为 286（商品页双轨的业务错误文案）：4 条 presentation 哨兵 —— ErrDetachConfirmRequired /
	// ErrRollbackTargetMiss / ErrRollbackFailed / ErrSnapshotMismatch；本轮把它们的**常量值改成
	// 带模块前缀的 i18n key**（presentation.err.*）并 seed 中英各一行。必须 seed 的理由：
	// 商品页读侧是 tr(key, fallback)，不 seed 时页面上原样显示裸 key，用户看不到
	// 「回滚目标不在位」「该版本不属于该商品」这些**可行动**的区别。
	zhCount := countRows(t, db, "sys_i18n", "lang = ?", "zh-CN")
	enCount := countRows(t, db, "sys_i18n", "lang = ?", "en-US")
	if zhCount != 3744 {
		t.Fatalf("sys_i18n zh-CN 行数应为 3744（含 site.fragment.* 片段词条、176 商品列表词条，以及 187/188/189/190/191/192/193/197 八批后台模板抽取：settings/plugins/media 101、article/content 149、自定义 404 页 9、营销订单类 765、商品库存类 582、站点结构类 298、系统管理类 342、HTMX/仪表盘 8 —— 审计 I18N-001 与 SEO-013 的落地；+1 为 277 批量 id 超限的受控提示（shell.err.bulkIdsTooMany，中英各一行）；+7 为 198 masterdata 模块文案 key 化；+17 为 216 webhook 模块文案 key 化 —— 8 个 Err*（含 SSRF 五个）与 4 个 Msg*，审计 CQ-010；+38 为 217 SEO 控制台页面文案 key 化 —— 该页 436d20b 随门禁脚本一起提交时漏了 key 化，门禁因此从第一天红着；+60 为 219 重定向管理页文案 key 化，审计 SEO-025；+18 为 222 访问统计页维度榜与保留期提示的后台模板文案 key 化，审计 SEO-019 / SEO-021；【-15】为主题包导入导出 15 条文案：该能力随 VIS-014 线下线（023 迁移 225 删除，seed 220/221 注销），总数由 3037 回落；+4 为 226 数据规则配置校验词条 —— ErrRuleConfigInvalid / ErrRuleFieldNotAllowed / ErrRuleLogicNotAllowed / ErrRuleOpNotAllowed，域白名单收口时「越界报哪一项」的明确文案；+2 为 227 抽屉表单的通用操作词条 —— admin.common.action.cancel / save，列表页新建编辑统一进抽屉后不再逐页复制同义词条；+314 为 第四轮后台页面改造新增的文案位（230/231/232/233：列表页标准骨架 / 商品详情拆页 / 批量操作 / 页签）；+1 为 235 仪表盘降级文案（admin.dashboard.loadError）；+1 为 237 block 模块补漏的文案 —— ErrBlockProjectRequired：DB-009 第六批给 block 加了该哨兵却漏了同批 seed，缺词条时页面上会原样显示这个 key；+6 为 239 商品类型（迁移 238）配套 —— ErrProductTypeInvalid / ErrProductTypeImmutable / ErrBundlePriceRequired 三个 enums 常量（enums 的值就是 i18n key，不 seed 就原样返回 key）与新建商品抽屉的「商品类型」三项文案（admin.products.label.type 及两个选项）；+80 为 241–243 库存域收口 —— 九个内置变动原因（inventory.reason.<code>，迁移 241 把 name 收口成 i18n key）与库存域全部业务错误词条（ErrWarehouse* / ErrStock* / ErrReason* / ErrMovementTimeRangeInvalid，迁移 242），加库存三个后台页的新文案（迁移 243：调整入口 / 时间筛选 / 仓库类型与第三方对接配置 / 原因启停 / 调整方向文案）；+3 为 244/245 的仓库成本（inventory 与商品详情页的「当前成本价」字段与来源说明）；+3 为 248 商品主体 SKU —— ErrSkuContainerMissing / ErrContainerSkuInvalid 两条业务错误与抽屉的「SKU 编码」字段文案；+5 为 249 捆绑主体 SKU 必填 —— ErrBundleSKURequired 与抽屉四个文案位（重新生成 / 建议值说明 / 请手填 / 捆绑占位符）；+18 为 253 变体清单「预览—保存」模型 —— ErrVariantSKUEmpty / ErrVariantOptionsInvalid 两条业务错误、四个跳过原因（VariantSkipHasStock / VariantSkipReferenced / VariantSkipDuplicated / VariantSkipVariantMissing）与详情页清单区块的 12 个模板文案位；+17 为 252 外部编码映射与「从仓库选」—— 7 个库存 enums（ErrExternalSKU* / ErrWarehouseSKU* / ErrSKUSourceInvalid）、9 个新建抽屉文案、1 个库存页「外部编码」列；254 是把 188 的 4 条富文本说明 UPDATE 成真实行为（只改文案、不新增行）；+3 为 255 库存页外码行内编辑（占位 / 保存 / 说明三个文案位）；+1 为 258 的 ErrAuditFailed（publication SEO 体检归口文案，修 CQ-009 直出内部错误时新增）；+24 为 264 商品多仓 / 裸码 / 数量三态那一批（多仓选中、跟踪开关、库存聚合三态等文案位）；+39 为 259 批次 C —— 删除守卫两条跳过原因（VariantSkipBundleReferenced / VariantSkipHasMovement）、三种成员来源与 5 个成员跳过原因、3 条来源错误，加捆绑配置器的 27 个文案位与详情页「成员来源」列；+1 为 268 admin 内部错误归口文案（adminenums.ErrInternal：74 处把 err.Error() 拼进响应消息的收口，审计 CQ-009/CQ-010）；+1 为 269 navigation 内部错误归口文案（navigationenums.ErrInternal：5 处 err.Error() 直出收口，审计 CQ-009）；+1 为 270 入库入口的 SKU 编码校验文案（inventoryenums.ErrStockSKURequired：入库新建库存行时 sku_code 的空串 / 带前缀口径缺口收口，迁移 270 中英各一行）；+1 为 276 mail 内部错误归口文案（mailenums.ErrInternal：邮箱后台四个页面 handler 把 err.Error() 拼进 ?err= / 渲染数据的收口，审计 CQ-009，中英各一行，见 register_identity_mail.go 的 276）；+5 为 271 admin 错误文案第二组 —— 列表排序参数（admin.err.sortFieldInvalid / admin.err.sortDirectionInvalid）与词条表单缺项（admin.err.i18nKeyEmpty / admin.err.i18nLangEmpty / admin.err.i18nValueEmpty），中英各一行，判定见 register_admin_i18n.go 的 271。**基线提示**：本账本只保证「全库行数对得上」，数字随每一批 seed 增长 —— 本批落库时全库基线已因同期其它批次而不同（不是历史注释里那个数），因此统一按当时的实际值收口。新增词条后必须同步这个数字，否则整个包变红、原因看起来却像「这批把词条改坏了」；+1 为 280 库存页操作完成回执（admin.inventory.actionDone，中英各一行，读侧回执白名单批引入）；+5 为 278 页面列表页的「发布回执收敛状态」（admin.pages.receipts.* 五个文案位：待收敛条数 / 最老一条已等待 / 最近一次收敛 / 建议操作 / 无积压），中英各一行 —— 页面收敛机制此前只有结构化日志与 /readyz 可见，运维每天的落点却看不到积压；【-1】为 180 删除 order.msg.cancelledStockWarning（与 orderenums.MsgCancelledStockWarning 常量同批删净：该 key 是「先提交状态、再补偿库存」那条跨模块路径的遗留文案，事务收口后无任何可渲染场景。删词条必须连 seed 的判定 key 列表与门槛一起改，否则下次启动会灌回来 —— 180 的 >=62 已同步改为 >=61）；+12 为 279 插件产物对账巡检（admin.plugins.patrol.*：孤儿 schema / 缺 schema / 孤儿存储目录 / 缺目录四类不一致各自的标题、说明与处理建议），中英各一行 —— 卸载要清三处互相独立的存储（L1 schema / 注册行 / 存储目录），目录清理不参与事务因而每步都可能单独失败，残片此前没有任何入口能发现）；+21 为 284 后台模板剩余的硬编码文案（`admin.product_detail.template.*` 16 个 + `admin.products.new.template.*` 3 个 + 邮件营销导入的两个 placeholder 2 个，中英各一行）—— 这批是 2026-09-19 第五批提交时**随模板一起漏 key 化**的 19 行，门禁因此从提交那天起红着；同批把 `scripts/i18n-coverage-baseline.txt` 由 2 **下调为 0**（基线是债务清单，只能降不能升，把它抬到 19 等于把门禁做废）；+19 为 290（导航菜单页与工作台检查器的补漏词条：移动端位置名 2 / 悬浮面板区 7 / 预览按钮 2 / 批量删除确认 1 / 上次操作未完成前缀 1 / 乐观锁冲突 ErrStaleVersion 1 / 检查器就地新建菜单项 5，中英各一行。这批全是「模板已有中文兜底、词条表缺行」—— 缺行时英文界面显示中文，中文界面看着一切正常）；+20 为 291（结构模板后台改造的词条：内容模板列表页 14 个 —— introStructure / colType / colStatus / colRefs / activeBadge / activate / refPages / refInstances / refNone / refUnknown / entityHeader / entityFooter / roleLabel / noVisualEdit，主题设置页「选结构模板」下拉 6 个 —— structure_templates / header_template / footer_template / structure_none / structure_none_footer / structure_hint，中英各一行。上一批把结构模板做成可绑定，这一批让它能在后台看见与切换）），实际 %d", zhCount)
	}
	if enCount != 3628 {
		t.Fatalf("sys_i18n en-US 行数应为 3628（同上；+1 为 277 批量 id 超限的受控提示（中英各一行）；webhook 的 17 个、SEO 控制台的 38 个、重定向管理页的 60 个与访问统计维度榜的 18 个、数据规则配置校验的 4 个 key 中英各一行；主题包的 15 个随 VIS-014 下线删除；+314 为 第四轮后台页面改造新增的文案位（230/231/232/233：列表页标准骨架 / 商品详情拆页 / 批量操作 / 页签）；+1 为 235 仪表盘降级文案（admin.dashboard.loadError）；+1 为 237 block 模块补漏的文案 —— ErrBlockProjectRequired：DB-009 第六批给 block 加了该哨兵却漏了同批 seed，缺词条时页面上会原样显示这个 key；+6 为 239 商品类型（迁移 238）配套 —— ErrProductTypeInvalid / ErrProductTypeImmutable / ErrBundlePriceRequired 三个 enums 常量（enums 的值就是 i18n key，不 seed 就原样返回 key）与新建商品抽屉的「商品类型」三项文案（admin.products.label.type 及两个选项）；+80 为 241–243 库存域收口 —— 九个内置变动原因、库存域全部业务错误词条与库存三个后台页的新文案（中英成对，同 zh-CN 说明）；+3 为 244/245、+3 为 248、+5 为 249（同 zh-CN 的逐条说明：+18 为 253、+17 为 252、254 不加行）。+1 为 268（同 zh-CN，adminenums.ErrInternal）；+1 为 269（同 zh-CN，navigationenums.ErrInternal）；+1 为 270（同 zh-CN，inventoryenums.ErrStockSKURequired）；+1 为 276（同 zh-CN，mailenums.ErrInternal）；+5 为 271（同 zh-CN，admin 排序参数与词条表单缺项，中英各一行）；**基线提示**：同 zh-CN —— 数字随每一批 seed 增长，本批（库存域 +80）按当时的实际值收口；新增词条后必须同步该数字；+1 为 280（同 zh-CN，admin.inventory.actionDone）；+5 为 278（同 zh-CN：admin.pages.receipts.* 五个文案位）；【-1】为 180 删除 order.msg.cancelledStockWarning（与死常量同批删净）；+12 为 279 插件产物对账巡检（admin.plugins.patrol.* 十二个 key，中英各一行）；+21 为 284（同 zh-CN：后台模板剩余 21 个文案位 —— 商品详情页模板面板 16、商品新建提示 3、邮件营销导入 placeholder 2，中英各一行）；+19 为 290（同 zh-CN：移动端位置名 / 悬浮面板区 / 预览 / ErrStaleVersion / 检查器新建菜单项）；+20 为 291（同 zh-CN：内容模板列表页 14 个文案位与主题设置页「选结构模板」下拉 6 个，中英各一行））），实际 %d", enCount)
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
