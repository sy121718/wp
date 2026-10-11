-- 593 · 区块页「JSON 写表单」的两条界面文案（2 个 key，中英成对）。
--
-- 背景：区块页的新建 / 删除 / 批量删除改为前端 fetch 直调现有 JSON API
-- （internal/templates/static/js/ui/json_write.js），结论不再由服务端渲染成整页提示，
-- 于是有两条文案必须由页面自己带下去：
--   · 批量删除结果区里的「刷新列表」入口 —— 刻意**不自动刷新**，让人先看完
--     「已删 N / 跳过 M + 逐条原因」再决定刷新（自动刷新会让结果一闪而过）；
--   · 无 JavaScript 时的提示 —— 写动作全部由浏览器发起，禁用 JS 时这些控件是 hidden 的，
--     没有这句话用户只看到「按钮不见了」，会以为是权限问题。
--
-- 其余文案**不新增 key**，全部复用既有词条（避免同一个意思有两份句子）：
--   · none-selected / all-deleted / all-skipped / partial → admin.blocks.bulkResult.*
--     （占位符正好是 {count} / {deleted} / {skipped}，与前端填充口径逐字一致）；
--   · 请求失败 → ErrInternal「操作失败，请稍后重试」；
--   · 批量超 200 条 → shell.err.bulkIdsTooMany（模板把成品句带进 data 属性，
--     前端按 (max, count) 顺序填 %s —— 与 response.translate 的 %s 位置填参同口径）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register 里**逐条枚举**
-- 本批的 2 个 item_key（上界封闭）——前缀 LIKE 会被同期其它批次的行满足而静默跳过，
-- 全库总量则会让本批每次启动重跑（本仓库两种都踩过，理由见 226/277/283/592）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.blocks.write.refresh', 'zh-CN', '刷新列表', 200, 'admin', '区块页批量删除结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.blocks.write.refresh', 'en-US', 'Refresh list', 200, 'admin', '区块页批量删除结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.blocks.write.noJs', 'zh-CN', '本页的新建与删除需要 JavaScript：写操作由浏览器直接调用接口完成。请开启 JavaScript 后重试。', 200, 'admin', '区块页禁用 JS 时的提示（blocks.html 的 noscript）', 1, now(), now()),
('admin.blocks.write.noJs', 'en-US', 'Creating and deleting blocks on this page requires JavaScript: the write actions are issued by the browser against the API. Please enable JavaScript and try again.', 200, 'admin', '区块页禁用 JS 时的提示（blocks.html 的 noscript）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
