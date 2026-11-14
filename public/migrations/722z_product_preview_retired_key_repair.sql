-- 722z · 商品「预览」与通用「预览动作」两组退役 key 的裁决修复（723 的前置）
--
-- 背景：723（商品剩余短键）把 admin.products.row.preview → pRowPrev、
-- admin.common.action.preview → uiActPrev 做机械改名。它的冲突守卫按设计拒绝
-- 「旧行与新行元数据不同」的形态，整批定位后回滚；真实开发库 artex 上这两组都命中了。
--
-- 1) admin.common.action.preview（旧）与 uiActPrev（新）
--    文案完全相同（预览 / Preview），差异只在 remark：旧行是 231 的播种说明
--    「通用动作：预览（行内不重复实体名）」，新行是 232 的「第四轮后台页面改造新增文案位」。
--    remark 只是播种来源说明，不参与页面渲染；改名保留新行、退役旧行即可，无用户可见语义丢失。
--
-- 2) admin.products.row.preview（旧）与 pRowPrev（新）
--    旧行文案是 316 的裁决结果：把 191 的「预览详情页」缩成「预览」（提交 679f4dac 记录的理由是
--    「操作列按钮文案太长，用户明确要求缩成预览」），模板兜底同样是「预览」。
--    723 的新键默认值取自 191 的原始文案（「预览详情页」），属于机械改名时把 316 的裁决丢了回去。
--    裁决：以 316 的短文案为准 —— 仍是 191 原始值且无编辑痕迹的 pRowPrev 行改回「预览 / Preview」，
--    再退役旧行；任一侧已被改成其他文案的行一律不动。
--
-- 前置条件（全部满足才动手，否则交 723 按原严格语义拒绝）：
--   · 旧行文案必须正好等于 316 的裁决值（预览 / Preview）；运营改过旧行就不匹配；
--   · 新行要么已是裁决值，要么仍是 723 的机械默认值且无编辑痕迹
--     （update_time 为空，或 create_time/update_time 都有值且相等，与 746a 判据一致）。
--   仅「新行仍是机械默认值」时也修复（旧行已不存在的新库），保证全新安装同样得到 316 的短文案。
--
-- 幂等：修复后两个分支都不再命中，注册侧 ConditionSQL 返回 1 → 跳过。
-- 顺序：722z 数字前缀排在 316 之后、723 之前，先归一文案再让 723 退役旧行；
--       与 746a 之于 747 同一手法，不放宽 723 的守卫。
WITH pairs(old_key, new_key) AS (VALUES
    ('admin.products.row.preview', 'pRowPrev'),
    ('admin.common.action.preview', 'uiActPrev')
), approved(new_key, lang, item_value, stale_value) AS (VALUES
    ('pRowPrev', 'zh-CN', '预览', '预览详情页'),
    ('pRowPrev', 'en-US', 'Preview', 'Preview detail page'),
    ('uiActPrev', 'zh-CN', '预览', NULL),
    ('uiActPrev', 'en-US', 'Preview', NULL)
), candidates AS MATERIALIZED (
    SELECT p.old_key, a.new_key, a.lang, a.item_value AS target_value,
           n.id AS new_id, n.item_value AS new_value,
           EXISTS (SELECT 1 FROM sys_i18n o
                   WHERE o.item_key = p.old_key AND o.lang = a.lang
                     AND o.item_value = a.item_value) AS old_present
    FROM pairs p
    JOIN approved a ON a.new_key = p.new_key
    JOIN sys_i18n n ON n.item_key = a.new_key AND n.lang = a.lang
    WHERE (a.stale_value IS NOT NULL
           AND n.item_value = a.stale_value
           AND (n.update_time IS NULL
                OR (n.create_time IS NOT NULL AND n.create_time = n.update_time)))
       OR (n.item_value = a.item_value
           AND EXISTS (SELECT 1 FROM sys_i18n o
                       WHERE o.item_key = p.old_key AND o.lang = a.lang
                         AND o.item_value = a.item_value))
), normalized AS (
    UPDATE sys_i18n n
       SET item_value = c.target_value, update_time = now()
      FROM candidates c
     WHERE n.id = c.new_id
       AND n.item_value <> c.target_value
    RETURNING n.id
), retired AS (
    DELETE FROM sys_i18n o
     USING candidates c
     WHERE o.item_key = c.old_key
       AND o.lang = c.lang
       AND o.item_value = c.target_value
    RETURNING o.id
)
SELECT (SELECT count(*) FROM normalized) AS normalized,
       (SELECT count(*) FROM retired) AS retired;
