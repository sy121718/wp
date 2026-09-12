-- 141 · 优惠码与核销记录（BIZ-1）。
--
-- 两张表：coupons 是券本身（口径 / 门槛 / 时间窗 / 次数上限），
-- coupon_redemptions 是「哪张券在哪一单上用掉」的凭据。
--
-- 三个设计决定：
--   1. **次数用尽由数据本身表达**（max_uses / used_count + 原子递增守卫），不存 status=exhausted 这种派生值；
--      status 只表达「运营有没有手动停用」，过期与用尽都是算出来的。
--   2. **幂等键落在数据库上**：唯一索引 (coupon_id, order_id) 让「同一单重复核销」
--      在并发下也只会成功一次 —— 靠应用层「先查再插」必然漏判。
--   3. **停用不删**：有核销记录的券删掉之后，那些记录会指向一张查不到的券，
--      对账时分不清是数据坏了还是券被删了（删除入口在服务层按此拒绝）。

CREATE TABLE IF NOT EXISTS coupons (
    id              BIGSERIAL PRIMARY KEY,
    project_id      UUID         NOT NULL,
    -- code 存归一化后的大写（归一化只发生在服务层一处）。
    code            VARCHAR(64)  NOT NULL,
    name            VARCHAR(120) NOT NULL DEFAULT '',
    -- discount_type=percent 时 discount_value 是**折扣力度**（减去小计的百分之多少，100 = 全免），
    -- 不是折后百分比：「打 8 折」与「减 80%」差着一个数量级，歧义必须在这里钉死。
    discount_type   VARCHAR(16)  NOT NULL,
    discount_value  BIGINT       NOT NULL,
    min_subtotal    BIGINT       NOT NULL DEFAULT 0,
    max_uses        INTEGER      NOT NULL DEFAULT 0,
    used_count      INTEGER      NOT NULL DEFAULT 0,
    per_user_limit  INTEGER      NOT NULL DEFAULT 0,
    starts_at       TIMESTAMP(3),
    ends_at         TIMESTAMP(3),
    status          SMALLINT     NOT NULL DEFAULT 1,
    remark          VARCHAR(255) NOT NULL DEFAULT '',
    create_by       BIGINT       NOT NULL DEFAULT 0,
    update_by       BIGINT       NOT NULL DEFAULT 0,
    create_time     TIMESTAMP(3) NOT NULL DEFAULT NOW(),
    update_time     TIMESTAMP(3) NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_coupons_discount_type CHECK (discount_type IN ('percent', 'fixed')),
    CONSTRAINT ck_coupons_discount_value CHECK (
        (discount_type = 'percent' AND discount_value BETWEEN 1 AND 100)
        OR (discount_type = 'fixed' AND discount_value > 0)
    ),
    CONSTRAINT ck_coupons_min_subtotal CHECK (min_subtotal >= 0),
    CONSTRAINT ck_coupons_limits CHECK (max_uses >= 0 AND per_user_limit >= 0 AND used_count >= 0),
    -- 结束不晚于开始 = 这张券永远不可能生效，存进去等于把活动悄悄废掉。
    CONSTRAINT ck_coupons_window CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at),
    CONSTRAINT ck_coupons_status CHECK (status IN (0, 1))
);

-- 工程内券码唯一（大小写不敏感：归一化之后仍然用 upper 兜一层，防止绕过服务层写入）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_coupons_project_code ON coupons (project_id, upper(code));
CREATE INDEX IF NOT EXISTS idx_coupons_project_status ON coupons (project_id, status);

CREATE TABLE IF NOT EXISTS coupon_redemptions (
    id              BIGSERIAL PRIMARY KEY,
    coupon_id       BIGINT       NOT NULL,
    project_id      UUID         NOT NULL,
    -- 券码快照：核销记录要能独立读出来，不依赖 coupons 行当前的样子。
    code            VARCHAR(64)  NOT NULL,
    order_id        BIGINT       NOT NULL,
    -- 订单号快照（同上：凭据自带识别信息）。
    order_no        VARCHAR(40)  NOT NULL DEFAULT '',
    discount_amount BIGINT       NOT NULL DEFAULT 0,
    -- 核销人：匿名下单时为 NULL（访客结算建号之后一般有值）。
    user_id         BIGINT,
    create_time     TIMESTAMP(3) NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_coupon_redemptions_amount CHECK (discount_amount >= 0)
);

-- 幂等键：同一张券在同一订单上只能核销一次（服务层用 ON CONFLICT DO NOTHING 命中它）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_coupon_redemptions_coupon_order ON coupon_redemptions (coupon_id, order_id);
CREATE INDEX IF NOT EXISTS idx_coupon_redemptions_project ON coupon_redemptions (project_id, coupon_id);
-- 每人限次查询走这条（coupon_id + user_id）。
CREATE INDEX IF NOT EXISTS idx_coupon_redemptions_user ON coupon_redemptions (coupon_id, user_id);
