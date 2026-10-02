-- 501_order_ship_country.sql
-- 订单地址补上「国家 / 地区」：收货与账单各一列（后端贯通，快照口径）。
--
-- 为什么是 orders 上的**快照列**，而不是「存 code 再去 sys_area 取名」的引用：
--   135 里地址的其余几段（ship_province / ship_city / ship_district / ship_address /
--   ship_zip 与对应的 bill_*）全是下单时刻的快照列 —— 订单要还原的是「当时是什么」。
--   国家列若改成引用语义，同一行地址里就会出现两种性质的数据：改字典会把历史订单的
--   收件国家一起改掉。因此这里存的是**下单时刻的国家代码快照**，与 sys_area 的当前行无关；
--   展示时按当前语言查 sys_area 取名只是**展示标签**（查不到就显示代码本身），
--   不参与订单语义，也不需要为此新增列。
--
-- 类型与默认值：VARCHAR(2) 装 ISO 3166-1 alpha-2（CN / US），
-- NOT NULL DEFAULT '' —— 与 135 的同一批地址列保持一致的「空串 = 未收集」口径，
-- 不引入 nullable：非中国站点与存量订单都可以没有它，而 NULL 与 '' 两种「没有」
-- 只会让下游每处判断都要写两遍。
--
-- 幂等：ADD COLUMN IF NOT EXISTS 重复执行安全（注册项的 CheckSQL 也按两列是否都已存在判定）。

ALTER TABLE orders ADD COLUMN IF NOT EXISTS ship_country VARCHAR(2) NOT NULL DEFAULT '';
ALTER TABLE orders ADD COLUMN IF NOT EXISTS bill_country VARCHAR(2) NOT NULL DEFAULT '';

COMMENT ON COLUMN orders.ship_country IS '收货地址国家/地区代码快照（ISO 3166-1 alpha-2，如 CN）；空 = 未收集（非中国站点 / 存量数据），不随字典变化';
COMMENT ON COLUMN orders.bill_country IS '账单地址国家/地区代码快照（ISO 3166-1 alpha-2，如 CN）；空 = 未收集，不随字典变化';
