-- 416 · 控件选型收口的三组词条（优惠码时间窗控件 / 内容模板实体分组 / 编辑抽屉字段标签）
--
-- 背景（审计 02-L §3 P2-14、02-O §3 任务 1 与任务 5）：三处「用错控件」的修正会改动
--   用户可见文案，而**模板里的中文只是 t() 兜底** —— 词条命中时显示的是库里的值。
--   只改模板不改词条，生产环境会继续显示旧文案；时间窗那一处更糟：旧文案描述的是
--   「手打格式」，换了日期选择器之后它就与控件直接矛盾。
--
--   ① 优惠码时间窗：<input type="text"> → 原生 <input type="datetime-local">。
--      文案从「用文本填写，两种写法都接受：2006-01-02 或 2006-01-02 15:04」改成
--      「用日期时间选择器挑选起止时刻」——格式说明不再需要，控件自己表达格式。
--      字段标题也去掉「如 2026-01-01 09:00」：原生控件不渲染 placeholder，
--      这两条词条现在只作可见 label 的文案来源。
--   ② 内容模板的实体类型下拉加 <optgroup>：新增「结构模板」「内容实体」两个分组标签。
--   ③ 优惠码编辑抽屉补 3 个字段标签（总次数上限 / 每人限次 / 备注）：同抽屉其它字段
--      都有 label，这三个只有 placeholder —— 填完值回看时 placeholder 已经消失。
--
-- 幂等：
--   · 新增走 INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；
--   · 修正走 UPDATE 且**带 item_value = '<旧值>' 条件**（与 399 同形）——只在值还是
--     旧文案时才改，运营在后台改过的词条不会被部署静默回滚。
--   判定写法：ConditionSQL 由迁移器 db.Raw 直接执行、没有任何参数替换，判定用的 key
--   只能写成 SQL 字面量（写成 ? 会被换成表名，判定恒为 0、每次启动重跑，178 踩过）。
--
-- 为什么这批 UPDATE 必须放在**种子**台账（registerSeed，见 register_control_selection_i18n.go）
--   而不是迁移台账：这 6 个 key 由 190（registerSeed）插入，而迁移台账整体先于种子台账
--   执行 —— 放迁移侧会对新库「先改后插」，UPDATE 一行都打不到。本票版本 416 > 190，
--   同一台账内按版本升序执行，于是「先插 190、后改 416」的顺序成立。
--
-- 只改这 6 个 key 的**值**、不增删行数：190 的门槛是「本批 765 个 key 的 zh-CN 行数
--   ≥ 765」，行数不变 → 190 不会因此重跑，也不会重插（重插也撞 ON CONFLICT DO NOTHING）。

-- 1) 新增词条（5 key × 2 语言 = 10 行）

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.coupons.detail.ph.max_uses', 'zh-CN', '总次数上限（0 = 不限）', 200, 'admin', 'admin/coupons.html: 编辑抽屉 maxUses 字段标签（原先只有 placeholder）', 1, now(), now()),
('admin.coupons.detail.ph.max_uses', 'en-US', 'Total usage limit (0 = unlimited)', 200, 'admin', 'admin/coupons.html: 编辑抽屉 maxUses 字段标签（原先只有 placeholder）', 1, now(), now()),
('admin.coupons.detail.ph.per_user_limit', 'zh-CN', '每人限次（0 = 不限）', 200, 'admin', 'admin/coupons.html: 编辑抽屉 perUserLimit 字段标签（原先只有 placeholder）', 1, now(), now()),
('admin.coupons.detail.ph.per_user_limit', 'en-US', 'Per-user limit (0 = unlimited)', 200, 'admin', 'admin/coupons.html: 编辑抽屉 perUserLimit 字段标签（原先只有 placeholder）', 1, now(), now()),
('admin.coupons.detail.ph.remark', 'zh-CN', '备注', 200, 'admin', 'admin/coupons.html: 编辑抽屉 remark 字段标签（原先只有 placeholder）', 1, now(), now()),
('admin.coupons.detail.ph.remark', 'en-US', 'Note', 200, 'admin', 'admin/coupons.html: 编辑抽屉 remark 字段标签（原先只有 placeholder）', 1, now(), now()),

('admin.content.templates.entityGroupStructure', 'zh-CN', '结构模板', 200, 'admin', 'admin/content_templates.html: 实体类型下拉的 optgroup 分组标签（页眉 / 页脚）', 1, now(), now()),
('admin.content.templates.entityGroupStructure', 'en-US', 'Structure templates', 200, 'admin', 'admin/content_templates.html: 实体类型下拉的 optgroup 分组标签（页眉 / 页脚）', 1, now(), now()),
('admin.content.templates.entityGroupContent', 'zh-CN', '内容实体', 200, 'admin', 'admin/content_templates.html: 实体类型下拉的 optgroup 分组标签（product / article）', 1, now(), now()),
('admin.content.templates.entityGroupContent', 'en-US', 'Content entities', 200, 'admin', 'admin/content_templates.html: 实体类型下拉的 optgroup 分组标签（product / article）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 2) 修正时间窗说明（旧文案描述的输入方式已随控件更换而消失）
--    组装后的英文：The time window is set with <b>the date-time picker</b> (read as the
--    site's local time): Blank means no limit on that side. The end must be later than ...

UPDATE sys_i18n
SET item_value = 'The time window is set with ', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.lead'
  AND lang = 'en-US'
  AND item_value = 'The time window is entered as ';

UPDATE sys_i18n
SET item_value = '日期时间选择器', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.strong'
  AND lang = 'zh-CN'
  AND item_value = '文本';

UPDATE sys_i18n
SET item_value = 'the date-time picker', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.strong'
  AND lang = 'en-US'
  AND item_value = 'plain text';

UPDATE sys_i18n
SET item_value = '挑选起止时刻（按站点本地时间理解）：', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.mid'
  AND lang = 'zh-CN'
  AND item_value = '填写，两种写法都接受：';

UPDATE sys_i18n
SET item_value = ' (read as the site''s local time): ', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.mid'
  AND lang = 'en-US'
  AND item_value = ', and both formats are accepted: ';

UPDATE sys_i18n
SET item_value = '留空 = 该侧不限。', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.mid2'
  AND lang = 'zh-CN'
  AND item_value = ' 或 ';

UPDATE sys_i18n
SET item_value = 'Blank means no limit on that side. ', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.mid2'
  AND lang = 'en-US'
  AND item_value = ' or ';

UPDATE sys_i18n
SET item_value = '结束时间必须晚于开始时间，否则这张券永远不会生效 —— 服务端会直接拒绝。', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.tail'
  AND lang = 'zh-CN'
  AND item_value = '，按站点本地时间理解（带时区的写法会被拒绝）。结束时间必须晚于开始时间，否则这张券永远不会生效 —— 服务端会直接拒绝。';

UPDATE sys_i18n
SET item_value = 'The end must be later than the start, otherwise the coupon never takes effect — the server rejects it outright.', update_time = now()
WHERE item_key = 'admin.coupons.create.time_hint.tail'
  AND lang = 'en-US'
  AND item_value = ', read as the site''s local time (an explicit timezone is rejected). The end must be later than the start, otherwise the coupon never takes effect — the server rejects it outright.';

-- 3) 修正时间窗字段标题：去掉原生控件用不到的格式示例
--    （datetime-local 不渲染 placeholder，这两条现在是可见 label 的文案来源）

UPDATE sys_i18n
SET item_value = '开始时间（留空 = 不限）', update_time = now()
WHERE item_key = 'admin.coupons.ph.starts_at'
  AND lang = 'zh-CN'
  AND item_value = '开始时间（留空 = 不限），如 2026-01-01 或 2026-01-01 09:00';

UPDATE sys_i18n
SET item_value = 'Start time (blank = no limit)', update_time = now()
WHERE item_key = 'admin.coupons.ph.starts_at'
  AND lang = 'en-US'
  AND item_value = 'Start time (blank = no limit), e.g. 2026-01-01 or 2026-01-01 09:00';

UPDATE sys_i18n
SET item_value = '结束时间（留空 = 不限）', update_time = now()
WHERE item_key = 'admin.coupons.ph.ends_at'
  AND lang = 'zh-CN'
  AND item_value = '结束时间（留空 = 不限），如 2026-01-31 23:59';

UPDATE sys_i18n
SET item_value = 'End time (blank = no limit)', update_time = now()
WHERE item_key = 'admin.coupons.ph.ends_at'
  AND lang = 'en-US'
  AND item_value = 'End time (blank = no limit), e.g. 2026-01-31 23:59';
