-- 057 · sys_menus 补 title_key 列（修现存 bug）
--
-- 背景：internal/module/admin/model/menu_model.go 已声明并 SELECT title_key，
-- 但 init_schema.sql 的 sys_menus 无该列 → 菜单查询/保存直接报列不存在。
-- 幂等：ADD COLUMN IF NOT EXISTS；
-- 注册见 register.go（CheckSQL 按 title_key 列是否存在判定）。
ALTER TABLE sys_menus ADD COLUMN IF NOT EXISTS title_key VARCHAR(100);
COMMENT ON COLUMN sys_menus.title_key IS 'i18n 标题 key（routes 接口按语言翻译，未命中回退 title）';
