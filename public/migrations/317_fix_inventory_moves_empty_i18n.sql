-- 317 · 修正库存流水空态文案（admin.inventory.moves.empty）
--
-- 缺陷：/admin/inventory 的空态文案写着「还没有库存流水 —— 先用上面的表单做一次入库。」
--       但该页**没有任何入库表单** —— 入库在采购入库页（/admin/inventory/purchases），
--       本页只保留盘点 / 报损的库存调整入口。用户照着这句话找表单，会在页面上找不到出路。
--
-- 根因：模板 internal/templates/admin/inventory.html:201 早已改成正确文案
--       （"还没有库存流水 —— 入库请从采购入库页收货，盘点 / 报损请走右上角的库存调整。"），
--       但词条的真源是数据库：模板里的中文只是 **t() 兜底**，词条命中时永远显示库里的值。
--       而 191 写下的旧值用的是 ON CONFLICT (item_key, lang) DO NOTHING ——
--       seed 是「默认值来源」不是「真相来源」（设计如此：运营在后台改过的词条
--       不能被下一次部署静默回滚）。于是改模板文案**不会**更新库里已存在的值，
--       必须由一条新迁移显式 UPDATE。
--
-- 判据（哪里看出模板与库里不一致）：
--   模板：grep 'moves.empty' internal/templates/admin/inventory.html
--   库里：SELECT item_value FROM sys_i18n WHERE item_key='admin.inventory.moves.empty' AND lang='zh-CN';
--   实测（2026-09 后台评审，浏览器读 .empty-desc）：页面显示旧文案。
--
-- 影响面：zh-CN 与 en-US 两行。英文原值同样指向不存在的表单
--         （"start by receiving stock with the form above."），一并修正。
--
-- 为什么用 UPDATE 而不是重写 INSERT ... DO NOTHING：
--   DO NOTHING 对已存在的行是 no-op，写成 INSERT 等于什么都没做（正是本缺陷的成因）。
--   这里要的是「修正一个已知的错误默认值」，且必须**不覆盖运营在后台改过的值** ——
--   所以 UPDATE 带 WHERE item_value = <旧值> 前置条件：只有当库里还是那条旧值时
--  才改；若运营已经手工改过（值已不同），保持他们的修改不动。
--
-- 幂等：条件命中旧值才更新；重复执行时旧值已不存在，影响 0 行，安全。
--       本条是修正既有默认值，不涉及能力删除，故无 register_retired_permission_test.go 那类反查。

-- zh-CN：改为与模板一致的指引（指向真实存在的入口）
UPDATE sys_i18n
SET item_value = '还没有库存流水 —— 入库请从采购入库页收货，盘点 / 报损请走右上角的库存调整。',
    update_time = now()
WHERE item_key = 'admin.inventory.moves.empty'
  AND lang = 'zh-CN'
  AND item_value = '还没有库存流水 —— 先用上面的表单做一次入库。';

-- en-US：同步修正（原文同样指向不存在的表单）
UPDATE sys_i18n
SET item_value = 'No stock movements yet — receive stock on the Purchase Receiving page; for counts or write-offs use Stock Adjustment in the top right.',
    update_time = now()
WHERE item_key = 'admin.inventory.moves.empty'
  AND lang = 'en-US'
  AND item_value = 'No stock movements yet — start by receiving stock with the form above.';
