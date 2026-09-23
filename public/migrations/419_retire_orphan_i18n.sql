-- 419 · 退役孤儿词条（7 个 key / 14 行）
--
-- 缺陷同 418（admin.customers.empty）：这些 key 在模板与代码里都没有引用者，词条却还在库里。
-- 孤儿词条会让「哪些词条还在用」无法从库里判断 —— 改文案时容易改错那一条。
--
-- 本批 7 个 key（中英各一行 = 14 行）：
--   · admin.product_categories.seo.unset / admin.product_brands.seo.unset
--     商品分类 / 商品品牌列表的 SEO 空值占位。上一轮按「同一张表里两种空值写法会让人以为
--     它们代表不同的状态」改成硬编码 `<span class="text-mute">—</span>`
--     （internal/templates/admin/product_categories.html 与 product_brands.html 的 SEO 列），
--     词条失去引用。
--   · admin.inventory.pagination.info / .more / .end
--     库存流水 / 货源 / 采购入库三页「降级分页条」的信息行与后缀。三页换成真源分页后，
--     信息行与翻页按钮统一走 shell.pagination.*（迁移 059 登记），本域 3 个 key 失去引用；
--     它唯一的 seed（412）随之整批退役 —— 412 的幂等条件恰好由这 3 个 key 构成，
--     没有可保留的判定，故 register_inventory_pagination_i18n.go 与
--     412_i18n_inventory_pagination.sql 已同批删除。
--   · admin.mail.campaign.page_prefix / .page_suffix
--     邮件活动页「假分页条」的页码前缀与「页」后缀。该页换成真分页条后失去引用。
--
-- 核实无引用的命令（本批执行时，除 seed SQL / 迁移自身 / 测试注释外为空）：
--   for k in admin.product_categories.seo.unset admin.product_brands.seo.unset \
--            admin.inventory.pagination.info admin.mail.campaign.page_prefix \
--            admin.mail.campaign.page_suffix; do rg -n --hidden -g '!.git' -F "$k" . ; done
--
-- 与 seed 的关系（AGENTS.md「删能力时要连 seed 的 SQL 与幂等条件一起收口」，122 号迁移的坑）：
--   · 190（register_admin_i18n.go）的幂等条件是「本批 key 全在库」（764 个逐条枚举），
--     其中含 campaign 的 2 个 key —— 只删行不改条件会让条件恒假、每轮启动重跑约 1500 行 INSERT。
--     本批同批把 2 个 key 移出列表、门槛 764 → 762，并删掉 190_i18n_seed_marketing.sql 里那 4 行，
--     于是条件列表里的 key 全部仍由 seed SQL 写入。
--     实测（本机库）：移除后的列表在 zh-CN 侧命中 762 行，条件重新成立。
--     （注：190 SQL 里当时还留着 418 已退役的 admin.customers.empty 两行，那是 418 批「遗留的一半」。
--     本迁移执行时它不在本轮范围，且它的条件列表本就已不含该 key，故不影响本条件的成立；
--     那两行 INSERT 已随后续批次从 190 删除，最终状态见 418_retire_customers_empty_i18n.sql 的注释。）
--   · 230 的幂等条件取的是 3 个**代表** key（admin.common.bulk.selectAll /
--     admin.dashboard.stat.projects / admin.product_categories.empty.title），不含 seo.unset ——
--     删除不影响它，故只删 230_i18n_seed_list_skeleton.sql 里的 4 行（覆盖数 77 → 75 key）。
--   · 412 整批退役，见上。
--
-- 为什么删除动作放在 **seed 台账**（registerSeed，见 register_orphan_i18n_retire.go）：
--   migrator 的两个循环独立，Migrations 全部先跑、Seeds 后跑 —— 在 register 台账里做的删除，
--   永远赢不过在 registerSeed 里重建它的 seed（AGENTS.md「数据库」一节的实测故障）。
--   用 seed 台账且版本号 419 > 190 / 230 / 412，保证每轮启动的净结果是「这些 key 不在库里」。
--
-- 幂等：DELETE 本身幂等（重复执行影响 0 行）。

DELETE FROM sys_i18n WHERE item_key IN (
    'admin.product_categories.seo.unset',
    'admin.product_brands.seo.unset',
    'admin.inventory.pagination.info',
    'admin.inventory.pagination.more',
    'admin.inventory.pagination.end',
    'admin.mail.campaign.page_prefix',
    'admin.mail.campaign.page_suffix'
);
