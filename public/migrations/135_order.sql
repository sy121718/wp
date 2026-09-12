-- 135_order.sql
-- 订单模块（BIZ-1 销售侧）：订单头 / 订单项快照 / 状态流转流水。
--
-- 字段清单参考 WooCommerce 的订单信息模型（billing/shipping 双地址、金额分解、支付流水号、
-- created_via 来源、下单 IP/UA 等），但**只取字段，不碰它的实现**：
--   · 不连它的库、不读 wp_* 表、不做数据迁移 —— 本文件只建 go_wp 自己的三张表；
--   · 不搬它的 postmeta 结构：WC 把账单邮箱 / 订单总额 / 支付方式 / 客户 id 全塞进
--     wp_postmeta 的 key-value（_billing_email、_order_total、_payment_method、_customer_user），
--     无 schema、无类型、索引不了；这里一律结构化列。
--   · 不搬它的订单项存法：WC 的名称与单价在 woocommerce_order_itemmeta 里；这里是结构化快照列。
--   · 不搬它的状态存法：WC 把状态挤进 posts.post_status（'wc-pending' 前缀串）；这里 status 是独立列。
-- 与 123_user.sql 对 wp_users 的态度一致：**能用列表达的就不许进 key-value**。
--
-- 金额一律**整数分**（BIGINT），不用浮点：财务口径下 0.1 + 0.2 的误差不是显示问题，是对账对不平。
--
-- 状态机取值（比 WC 细一档：WC 用 processing 一个状态涵盖「已付款待发货」，这里拆开）：
--   pending 待付款 → paid 已付款 → shipped 已发货 → completed 已完成
--   pending（未付款）/ paid / shipped 可 → cancelled 已取消
--   paid / shipped / completed 可 → refunded 已退款
-- 合法流转边由 service 的状态机判定，本表只约束取值范围。

-- 1. orders —— 订单头
CREATE TABLE IF NOT EXISTS orders (
    id                   BIGSERIAL    PRIMARY KEY,
    project_id           UUID         NOT NULL,
    order_no             VARCHAR(40)  NOT NULL,
    status               VARCHAR(20)  NOT NULL DEFAULT 'pending',
    -- 归属：注册用户可为 NULL（游客下单是标配，不是异常路径）。只存 id 不建外键 ——
    -- 订单不因账号注销而消失。
    user_id              BIGINT,
    -- 客户信息**快照**（与 user 表当前值无关：改邮箱不该改写历史订单的收件人）
    customer_email       VARCHAR(120) NOT NULL DEFAULT '',
    customer_name        VARCHAR(60)  NOT NULL DEFAULT '',
    customer_phone       VARCHAR(40)  NOT NULL DEFAULT '',
    currency             VARCHAR(8)   NOT NULL DEFAULT 'CNY',
    -- 金额分解（分）：小计 / 优惠 / 运费 / 税 / 应付总额
    subtotal             BIGINT       NOT NULL DEFAULT 0,
    discount_total       BIGINT       NOT NULL DEFAULT 0,
    shipping_total       BIGINT       NOT NULL DEFAULT 0,
    tax_total            BIGINT       NOT NULL DEFAULT 0,
    total                BIGINT       NOT NULL DEFAULT 0,
    -- 收货地址快照
    ship_name            VARCHAR(60)  NOT NULL DEFAULT '',
    ship_phone           VARCHAR(40)  NOT NULL DEFAULT '',
    ship_province        VARCHAR(40)  NOT NULL DEFAULT '',
    ship_city            VARCHAR(40)  NOT NULL DEFAULT '',
    ship_district        VARCHAR(40)  NOT NULL DEFAULT '',
    ship_address         VARCHAR(255) NOT NULL DEFAULT '',
    ship_zip             VARCHAR(20)  NOT NULL DEFAULT '',
    -- 账单地址快照：与收货分开是有意的 —— 发票抬头地址与收货地址经常不是一处
    bill_name            VARCHAR(60)  NOT NULL DEFAULT '',
    bill_phone           VARCHAR(40)  NOT NULL DEFAULT '',
    bill_province        VARCHAR(40)  NOT NULL DEFAULT '',
    bill_city            VARCHAR(40)  NOT NULL DEFAULT '',
    bill_district        VARCHAR(40)  NOT NULL DEFAULT '',
    bill_address         VARCHAR(255) NOT NULL DEFAULT '',
    bill_zip             VARCHAR(20)  NOT NULL DEFAULT '',
    -- 支付：方式 code / 方式显示名 / 支付平台流水号（对账要按它查）
    payment_method       VARCHAR(40)  NOT NULL DEFAULT '',
    payment_method_title VARCHAR(60)  NOT NULL DEFAULT '',
    transaction_id       VARCHAR(120) NOT NULL DEFAULT '',
    paid_at              TIMESTAMP(3),
    completed_at         TIMESTAMP(3),
    -- 来源与审计：下单入口 + 访客 IP / UA（风控与纠纷取证都要）
    created_via          VARCHAR(20)  NOT NULL DEFAULT 'checkout',
    ip_address           VARCHAR(50)  NOT NULL DEFAULT '',
    user_agent           VARCHAR(255) NOT NULL DEFAULT '',
    -- 归因冗余（JSONB）：下单时刻的流量来源 / 广告参数 / 会话信息 / 下单前浏览轨迹。
    --
    -- 为什么是冗余 JSON 而不是几张关联表：归因数据**天生是快照**且形状不稳定
    -- （各广告平台的点击 id 各不相同、UTM 参数会被随手追加），拆表会立刻长出一张
    -- key-value 表 —— 那正是 WP postmeta 的老路。这些字段只用于分析与对账，
    -- 不参与业务条件查询，所以集中冗余成一列。
    --
    -- 字段名与语义对照 WooCommerce 核心的 _wc_order_attribution_* 与它背后的
    -- Sourcebuster.js（sbjs_* cookie），结构见 orderdto.Attribution：
    --   source_type / referrer
    --   utm{source,medium,campaign,content,term,id,platform,creative_format,marketing_tactic}
    --   ad{gclid,fbclid,ttclid,msclkid,click_id}
    --   session{entry,pages,count,start_time,duration_seconds}
    --   device{type,user_agent,screen}
    --   landing（首次落地页）
    --   trail[]（下单前若干分钟内的浏览轨迹：url / title / at / seconds）
    attribution          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    -- 后台备注：自建订单（created_via=admin）与代发订单在这里写补充说明。
    -- 与客户自己填的 remark 分成两列 —— 客户看到的不该是内部备注，反之亦然。
    admin_note           VARCHAR(500) NOT NULL DEFAULT '',
    -- 幂等键：同一 request_id 重复提交只落一单（与采购入库 receipts.request_id 同口径）
    request_id           VARCHAR(64)  NOT NULL DEFAULT '',
    remark               VARCHAR(255) NOT NULL DEFAULT '',
    cancel_reason        VARCHAR(255) NOT NULL DEFAULT '',
    create_by            BIGINT       NOT NULL DEFAULT 0,
    create_time          TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time          TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uk_orders_no UNIQUE (order_no)
);

CREATE UNIQUE INDEX IF NOT EXISTS uk_orders_request_id    ON orders (request_id) WHERE request_id <> '';
CREATE INDEX        IF NOT EXISTS idx_orders_project_status ON orders (project_id, status);
CREATE INDEX        IF NOT EXISTS idx_orders_user          ON orders (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX        IF NOT EXISTS idx_orders_customer_email ON orders (customer_email);
CREATE INDEX        IF NOT EXISTS idx_orders_transaction   ON orders (transaction_id) WHERE transaction_id <> '';
CREATE INDEX        IF NOT EXISTS idx_orders_create_time   ON orders (create_time DESC);

-- 2. order_items —— 订单项（下单时刻的**快照**）
-- 商品改名改价绝不能改写历史订单，所以展示与计价一律读本行的快照列；
-- product_id / variant_id 只作追溯用（「这是哪件商品的单」），不参与渲染。
CREATE TABLE IF NOT EXISTS order_items (
    id            BIGSERIAL    PRIMARY KEY,
    order_id      BIGINT       NOT NULL,
    product_id    UUID         NOT NULL,
    variant_id    UUID         NOT NULL,
    product_name  VARCHAR(200) NOT NULL DEFAULT '',
    variant_label VARCHAR(200) NOT NULL DEFAULT '',
    sku           VARCHAR(80)  NOT NULL DEFAULT '',
    -- 单价（分），优惠前
    unit_price    BIGINT       NOT NULL DEFAULT 0,
    quantity      INTEGER      NOT NULL DEFAULT 0,
    -- 金额分解（分）：行小计（单价×数量，优惠前）/ 行优惠 / 行税 / 实付小计
    line_subtotal BIGINT       NOT NULL DEFAULT 0,
    line_discount BIGINT       NOT NULL DEFAULT 0,
    line_tax      BIGINT       NOT NULL DEFAULT 0,
    line_total    BIGINT       NOT NULL DEFAULT 0,
    -- 成本快照（分）：毛利按下单时刻的成本算，不能按商品当前成本倒推
    cost_price    BIGINT       NOT NULL DEFAULT 0,
    create_time   TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_order_items_order   ON order_items (order_id);
CREATE INDEX IF NOT EXISTS idx_order_items_variant ON order_items (variant_id);

-- 3. order_status_logs —— 状态流转流水（append-only 审计）
-- 终态推不出路径（同一「已取消」可能来自待付款也可能来自已发货），所以每次流转都留痕。
CREATE TABLE IF NOT EXISTS order_status_logs (
    id            BIGSERIAL    PRIMARY KEY,
    order_id      BIGINT       NOT NULL,
    from_status   VARCHAR(20)  NOT NULL DEFAULT '',
    to_status     VARCHAR(20)  NOT NULL DEFAULT '',
    operator_type VARCHAR(20)  NOT NULL DEFAULT 'system',
    operator_id   BIGINT       NOT NULL DEFAULT 0,
    operator_name VARCHAR(60)  NOT NULL DEFAULT '',
    remark        VARCHAR(255) NOT NULL DEFAULT '',
    create_time   TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_order_status_logs_order ON order_status_logs (order_id, id);
