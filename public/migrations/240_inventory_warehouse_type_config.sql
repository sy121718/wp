-- 240 · 仓库类型与第三方对接配置（库存域收口）。
--
-- 产品决策：仓库不止「默认虚拟仓」一种 ——
--   · type 表达仓库的**业务角色**：
--       self        自营仓：自有实体仓，可收货可发货；
--       third_party 第三方仓：由外部服务商运营，各家对接方式不同；
--       virtual     虚拟仓：只用于分组 / 记账占位（在途、质检待处理等），没有实体收发能力；
--   · third_party 的 config 承载**每家不一样**的配置：对接方、外部仓代码、地址联系人、
--     是否允许发货等非敏感项明文存；凭据只以密文（apiKeyCipher，AES-256-GCM）或
--     引用名（secretRef）落库 —— 明文凭据既不落库也不回显，见
--     internal/module/product/inventory/service/inventory_warehouse_config.go。
--
-- 存量行的类型判定：**一律 self（自营）**，包括 is_default 那一行。
--   理由：默认仓是「未指定仓库」时的兜底，也是 SKU 编码前缀（{仓短码}_{商品码}_{序号}）的来源，
--   必须是**有实体收发能力**的仓；虚拟仓没有实体收发能力，把兜底落到虚拟仓上会让库存记进
--   一个永远不出货的仓（service 因此也拒绝把虚拟仓设为默认仓，见 ErrWarehouseTypeVirtualDefault）。
--   存量里没有任何第三方仓的证据（config 为空、也没有对接字段），故按自营实体仓处理；
--   将来真要区分，由运营在仓库管理页改类型即可，不需要再从迁移里猜。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + DROP CONSTRAINT IF EXISTS → ADD CONSTRAINT。

ALTER TABLE inventory_warehouses ADD COLUMN IF NOT EXISTS type text;
UPDATE inventory_warehouses SET type = 'self' WHERE type IS NULL OR type = '';
ALTER TABLE inventory_warehouses ALTER COLUMN type SET DEFAULT 'self';
ALTER TABLE inventory_warehouses ALTER COLUMN type SET NOT NULL;

ALTER TABLE inventory_warehouses DROP CONSTRAINT IF EXISTS inventory_warehouses_type_check;
ALTER TABLE inventory_warehouses
    ADD CONSTRAINT inventory_warehouses_type_check
    CHECK (type IN ('self', 'third_party', 'virtual'));

ALTER TABLE inventory_warehouses ADD COLUMN IF NOT EXISTS config jsonb;
UPDATE inventory_warehouses SET config = '{}'::jsonb WHERE config IS NULL;
ALTER TABLE inventory_warehouses ALTER COLUMN config SET DEFAULT '{}'::jsonb;
ALTER TABLE inventory_warehouses ALTER COLUMN config SET NOT NULL;
-- 注释：config 是「将来扩展的任意字段」的载体（jsonb），服务层按已知键合并写入、
-- 未识别的键原样保留 —— 加一家新的对接方不需要改表结构。