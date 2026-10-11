-- 599 · 商品捆绑页「写失败 + 重读也失败」档的三条界面文案（3 个 key，中英成对）。
--
-- 背景（task-38 的 Sol P2）：ProductBundleSave 写失败时要**原地重渲染**保住用户刚配的成员
-- 清单；如果这次重建又读不到配置（双失败档），页面必须
--   · 说清「输入留下了、但本次不能保存」——直接 .Key 的缺键会让 Jet 抛运行时错误，
--     整块响应被丢弃（比白页更难查），所以这三个 key 不是装饰，是这条路径的组成部分；
--   · 禁掉保存（配置读不出来就没法保证提交的是完整配置，宁可让用户先重新载入）；
--   · 给一条现有通道的「重新载入」入口（本页没有别的读路径）。
--
-- 其余文案**不新增 key**，全部复用既有词条（同一个意思不留两份句子）：
--   · 装载失败标题 / 说明 → admin.common.list.loadFailedTitle / loadFailedDesc；
--   · 这次保存为什么没通过 → admin.product_bundle.saveError。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（不覆盖运营改过的翻译）。
-- 门槛判据在 register 里**逐条枚举**本批的 3 个 item_key（上界封闭）——前缀 LIKE 会被同期
-- 其它批次的行满足而静默跳过，全库总量会让本批每次启动重跑（两种都踩过，见 226/277/283/592）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.product_bundle.reload', 'zh-CN', '重新载入', 200, 'admin', '捆绑页双失败档的重新载入入口（product_bundle_form.html）', 1, now(), now()),
('admin.product_bundle.reload', 'en-US', 'Reload', 200, 'admin', '捆绑页双失败档的重新载入入口（product_bundle_form.html）', 1, now(), now()),
('admin.product_bundle.cfg.saveBlocked', 'zh-CN', '保存配置（页面数据未读出）', 200, 'admin', '捆绑页双失败档禁用的保存按钮（product_bundle_form.html）', 1, now(), now()),
('admin.product_bundle.cfg.saveBlocked', 'en-US', 'Save (page data unavailable)', 200, 'admin', '捆绑页双失败档禁用的保存按钮（product_bundle_form.html）', 1, now(), now()),
('admin.product_bundle.reloadFailedKeepInput', 'zh-CN', '下面是你刚提交的成员清单与数值：本次禁止保存（配置没读出来，保存会把不完整的配置写进去），网络恢复后点「重新载入」再保存。', 200, 'admin', '捆绑页双失败档「输入已留下、本次不能保存」的说明（product_bundle_form.html）', 1, now(), now()),
('admin.product_bundle.reloadFailedKeepInput', 'en-US', 'Below is what you just submitted (members and values). Saving is disabled because the configuration could not be read — saving now would write an incomplete configuration. Reload once the site is reachable and save again.', 200, 'admin', '捆绑页双失败档「输入已留下、本次不能保存」的说明（product_bundle_form.html）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
