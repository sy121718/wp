-- 651 · 调拨单审批（B5）：requested / rejected 两态 + 申请人 / 审批人 / 驳回原因。
--
-- 状态机（对齐 ERP 迁移 168 的 prd_transfer 五态，扩展 630 的三态）：
--
--   requested ──Approve──→ pending ──Ship(需出货凭证)──→ in_transit ──Receive(需签收凭证)──→ completed
--       │                    │
--       ├──Reject──→ rejected
--       └──Cancel──────────→ cancelled（pending 也可取消）
--
-- 为什么建单一律进 requested（而不是给个「免审批」开关）：
--   审批的意义就是「出货前有人同意」。留一条绕过审批的路径，等于把「这批货该不该动」
--   的判定分散到两个地方 —— 而出货那一刻才是真正不可逆的时点。
--
-- 为什么存 rejected_reason 为纯文本而不是富文本 HTML：
--   go_wp 后台没有富文本编辑器组件，驳回原因是运营写给申请人的一句话，
--   落 HTML 就要引入 sanitize 通道（白名单一旦漂移就是存储型 XSS）。
--   纯文本由模板自动转义，等价表达、没有额外攻击面。
--
-- 幂等：ADD COLUMN IF NOT EXISTS / DROP CONSTRAINT IF EXISTS → ADD CONSTRAINT。
-- 存量单据回填：requested_at 取 create_time、requested_by 取 create_by
--   （630 时代没有审批环节，那些单据的「申请人」就是建单人）。

ALTER TABLE inventory_transfers
    ADD COLUMN IF NOT EXISTS requested_by bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS requested_at timestamptz,
    ADD COLUMN IF NOT EXISTS approved_by bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS approved_at timestamptz,
    ADD COLUMN IF NOT EXISTS approved_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS rejected_by bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rejected_at timestamptz,
    ADD COLUMN IF NOT EXISTS rejected_name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS rejected_reason text NOT NULL DEFAULT '';
-- 申请人姓名走 create_by_name 之外的独立列？不加：630 的 create_by 就是申请人，
-- 姓名由页面按 id 就地显示（与库存流水页同一口径）—— 只有审批 / 驳回两处需要
-- 「谁批的」这种事后追责文本，才存快照姓名。

UPDATE inventory_transfers SET requested_by = create_by WHERE requested_by = 0 AND create_by <> 0;
UPDATE inventory_transfers SET requested_at = create_time WHERE requested_at IS NULL;

ALTER TABLE inventory_transfers DROP CONSTRAINT IF EXISTS ck_inventory_transfers_status;
ALTER TABLE inventory_transfers
    ADD CONSTRAINT ck_inventory_transfers_status
    CHECK (status IN ('requested', 'pending', 'in_transit', 'completed', 'rejected', 'cancelled'));

-- 待审批是「需要人做事」的队列，列表页默认按它筛；单列部分索引比全表扫状态更贴用法。
CREATE INDEX IF NOT EXISTS idx_inventory_transfers_requested
    ON inventory_transfers (project_id, create_time DESC) WHERE status = 'requested';

COMMENT ON COLUMN inventory_transfers.status IS
    '调拨单状态：requested 待审批 / pending 待出货 / in_transit 在途 / completed 已完成 / rejected 已驳回 / cancelled 已取消';
COMMENT ON COLUMN inventory_transfers.requested_by IS '申请人（建单人）：0 = 630 时代的存量单据未记';
COMMENT ON COLUMN inventory_transfers.approved_by IS '审批人：0 表示尚未审批（或被驳回）';
COMMENT ON COLUMN inventory_transfers.rejected_reason IS '驳回原因（纯文本，运营填给申请人看）；仅 rejected 态有值';
