-- 237 · block 模块补一条被漏掉的词条：ErrBlockProjectRequired。
--
-- 背景：本项目里 enums 常量的值就是 i18n key（真文案在 sys_i18n，见 058）。DB-009 第六批
-- （c57448f）给 block 加了 ErrProjectRequired 哨兵 —— 「只带 id 的入口逐工程定位归属时，
-- 工程清单为空或读不到」这种显式失败 —— 但词条没有同批 seed。缺词条的后果是确定的：
-- 任何把它展示到页面/接口的地方都会原样显示 ErrBlockProjectRequired（既不是中文也不是话），
-- 而这类「只加常量不加词条」的遗漏不会有任何测试变红（058 的注释已就此立过规矩）。
--
-- 归属：internal/module/block/enums（block_enums.go 的 ErrBlockProjectRequired）。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源，
--   运营在后台改过的词条不会被下一次迁移覆盖。
-- 判定限定在自己的 key 上：不数全库行数（058 那种「全库 zh-CN 计数」在存量库永远判定已灌满，
--   新词条不会被灌进去）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrBlockProjectRequired', 'zh-CN', '缺少可作用域的站点工程，无法定位全局块的工程归属', 400, 'block', 'internal/module/block/enums', 1, now(), now()),
('ErrBlockProjectRequired', 'en-US', 'No site project in scope to resolve the block owner', 400, 'block', 'internal/module/block/enums', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
