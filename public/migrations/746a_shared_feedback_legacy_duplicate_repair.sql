-- 746a · 共享反馈六组：退役 key 与已存在新 key 的重复行修复
--
-- 问题形态（真实开发库 artex 上观察到，非人为编辑）：
--   · 早期的短键迁移把 MsgInternalError 的 en-US 行复制成 uiInternalErr/en-US；
--   · 之后 407 又用另一套文案重新播种了 MsgInternalError/en-US（ON CONFLICT DO NOTHING，
--     但那一行此前已被上一轮种子清理删除，所以它被重新创建）；
--   · 结果：退役 key 与新 key 同时存在且文案不同 —— 747 的冲突检查按设计拒绝整批并定位
--     `old -> new/lang`，绝不静默二选一。行为正确，但这份重复行需要一条显式修复路径。
--
-- 本迁移只解决**同一条 (退役 key, 新 key, 语言) 同时存在**这一种形态，且前置条件全部满足才动手：
--   1) 目标语言在 747 的规范默认表里有定义；
--   2) 两行文案不同，且恰好一行等于规范默认值；
--   3) 待删除的非规范行没有编辑痕迹：update_time 为空，或非空且等于 create_time。
-- 不满足时本迁移不动该对记录，由 747 按原有严格语义处理并报告可定位冲突。
--
-- 解法：只删掉符合上述条件的非规范副本，保留规范行的 id、元数据和历史。
-- 删除后 747 只需处理「只剩退役行」的语言，或直接跳过；缺失默认由 747/738 补。
--
-- 为什么必须排在 747 之前：747 的守卫会在本迁移之前整批拒绝。版本槽 746a 排序在 746 之后、
-- 747 之前（compareVersion 按 主版本 + 字母后缀 比较）。
--
-- 幂等：没有重复行时 `dup` 为空，删除语句不命中任何行；CheckSQL 在有重复行时才返回 0。
WITH pairs(old_key, new_key) AS (VALUES
    ('MsgInternalError', 'uiInternalErr'),
    ('ErrInvalidParams', 'sysErrBadParam'),
    ('ErrPermissionDenied', 'sysErrNoPerm'),
    ('ErrRequestEntityTooLarge', 'sysErrTooLarge'),
    ('msg_operation_success', 'uiOpSuccess'),
    ('msg_delete_confirm', 'uiDelConfirm')
), canonical(new_key, lang, item_value) AS (VALUES
    ('uiInternalErr', 'zh-CN', '系统内部错误，请稍后重试'),
    ('uiInternalErr', 'en-US', 'Internal error, please try again later'),
    ('sysErrBadParam', 'zh-CN', '请求参数错误'),
    ('sysErrBadParam', 'en-US', 'Invalid request parameters'),
    ('sysErrNoPerm', 'zh-CN', '无权限访问'),
    ('sysErrNoPerm', 'en-US', 'Permission denied'),
    ('sysErrTooLarge', 'zh-CN', '请求体过大'),
    ('sysErrTooLarge', 'en-US', 'Request entity too large'),
    ('uiOpSuccess', 'zh-CN', '操作成功'),
    ('uiOpSuccess', 'en-US', 'Operation successful'),
    ('uiDelConfirm', 'zh-CN', '确定要删除吗？'),
    ('uiDelConfirm', 'en-US', 'Are you sure to delete?')
), dup AS MATERIALIZED (
    SELECT p.old_key, p.new_key, o.lang,
           o.id AS old_id, n.id AS new_id,
           o.item_value AS old_value, n.item_value AS new_value,
           c.item_value AS canonical_value,
           -- 「无编辑痕迹」按**是否留下编辑痕迹**判断，不按时间戳齐全判断：
           --   · update_time 为空 —— 从未经 admin 写入（UpdateEntry 总会写 update_time），
           --     种子省略时间列时就是这个形态；
           --   · update_time 等于 create_time —— 种子在同一条 INSERT 里写 now(), now()。
           -- 两个 NULL 相等比较会得 NULL，参与 NOT/AND 后被静默吞掉；因此两项都显式判空：
           --   pristine = update_time 为空(从未经 admin 写入)
           --            OR (create_time 与 update_time 都有值且相等)。
           -- 只要 update_time 有值而 create_time 为空/C 不同，就视为**有编辑痕迹**。
           (o.update_time IS NULL
            OR (o.create_time IS NOT NULL AND o.create_time = o.update_time)) AS old_pristine,
           (n.update_time IS NULL
            OR (n.create_time IS NOT NULL AND n.create_time = n.update_time)) AS new_pristine
    FROM pairs p
    JOIN sys_i18n o ON o.item_key = p.old_key
    JOIN sys_i18n n ON n.item_key = p.new_key AND n.lang = o.lang
    LEFT JOIN canonical c ON c.new_key = p.new_key AND c.lang = o.lang
), classified AS MATERIALIZED (
    -- 只修**一种**形态：文案不同，且恰好一边等于 747 的规范默认值（另一边是陈旧副本）。
    -- 这一种是历史迁移留下的重复；其余一律不碰，交 747 按原语义整批拒绝：
    --   · 两边文案相同、元数据/时间不同 —— 747 的严格去重语义（不丢历史），本迁移不分流；
    --   · 两边文案都不是规范值 —— 运营各写过一版，绝不替人二选一；
    --   · 该语言不在规范表里 —— 不知道哪边才对。
    --
    -- 「陈旧副本」还要求它没有编辑痕迹：update_time 为空（种子省略时间列）或与
    -- create_time 相等。注意 NULL = 比较会得 NULL，因此两项都显式判空，
    -- 不能写成 `create_time = update_time` 后直接参与 AND。
    SELECT d.old_key, d.new_key, d.lang,
           CASE
               -- 退役行是规范值、新行是陈旧副本：删新行，让 747 原位搬迁退役行。
               WHEN d.old_value = d.canonical_value
                    AND d.new_value IS DISTINCT FROM d.canonical_value
                    AND (d.new_pristine) THEN d.new_id
               -- 新行是规范值、退役行是陈旧副本：删退役行，已在位的新行连同历史保留。
               WHEN d.new_value = d.canonical_value
                    AND d.old_value IS DISTINCT FROM d.canonical_value
                    AND (d.old_pristine) THEN d.old_id
           END AS id
    FROM dup d
    WHERE d.canonical_value IS NOT NULL
      AND d.old_value IS DISTINCT FROM d.new_value
      AND (d.old_value = d.canonical_value OR d.new_value = d.canonical_value)
)
DELETE FROM sys_i18n
WHERE id IN (SELECT id FROM classified WHERE id IS NOT NULL);
