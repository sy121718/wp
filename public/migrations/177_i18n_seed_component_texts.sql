-- 177 · i18n 词条 seed（组件固定文案补全，审计 I18N-010）
--
-- 覆盖：26 个 key / zh-CN 26 行 / en-US 26 行（中英均为人工编写，非机器伪造）。
-- 组件：product / searchResults / orderList / userForms / cardStack。
--   这些组件的固定文案已抽成 site.component.* key 并在构建期按语言取词，但词条是本次才落库的：
--   此前查不到词条就回退中文兜底 —— 中文站点看不出问题，英文站点整块仍是中文。
-- 命名：site.component.{组件}.{语义}（docs/06-D §10.3）。
-- 占位符：仅 %s（userForms 两句与 cardStack.zoomCardAria 各带一个）。
-- 语义（审计 I18N-003）：ON CONFLICT DO NOTHING —— seed 是**默认值来源**，不是真相来源。
--   后台改过的词条不会被下一次迁移覆盖（DO UPDATE 的旧写法会让运营的修改在下次部署时
--   静默回滚，而「我明明改过」这种问题极难定位）。要改默认值请改这里的 item_value 并删除
--   对应行后重跑，或在后台直接修改。
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE；注册见 register.go（Seed，按本批 key 集合判定）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.component.product.stockNote', 'en-US', 'Availability confirmed at checkout', 200, 'ui', 'internal/builder/components/product/{i18n.go,product.jet}', 1, now(), now()),
('site.component.product.stockNote', 'zh-CN', '以结算时库存为准', 200, 'ui', 'internal/builder/components/product/{i18n.go,product.jet}', 1, now(), now()),
('site.component.searchResults.label', 'en-US', 'Search', 200, 'ui', 'internal/builder/components/searchresults/{i18n.go,search_widget.jet}', 1, now(), now()),
('site.component.searchResults.label', 'zh-CN', '搜索', 200, 'ui', 'internal/builder/components/searchresults/{i18n.go,search_widget.jet}', 1, now(), now()),
('site.component.searchResults.submit', 'en-US', 'Search', 200, 'ui', 'internal/builder/components/searchresults/{i18n.go,search_widget.jet}', 1, now(), now()),
('site.component.searchResults.submit', 'zh-CN', '搜索', 200, 'ui', 'internal/builder/components/searchresults/{i18n.go,search_widget.jet}', 1, now(), now()),
('site.component.orderList.loginHint', 'en-US', 'Sign in to view your orders.', 200, 'ui', 'internal/builder/components/orderlist/{i18n.go,orders_widget.jet}', 1, now(), now()),
('site.component.orderList.loginHint', 'zh-CN', '登录后可以查看你的订单。', 200, 'ui', 'internal/builder/components/orderlist/{i18n.go,orders_widget.jet}', 1, now(), now()),
('site.component.orderList.loginText', 'en-US', 'Sign in', 200, 'ui', 'internal/builder/components/orderlist/{i18n.go,orders_widget.jet}', 1, now(), now()),
('site.component.orderList.loginText', 'zh-CN', '去登录', 200, 'ui', 'internal/builder/components/orderlist/{i18n.go,orders_widget.jet}', 1, now(), now()),
('site.component.orderList.pagesText', 'en-US', 'Orders', 200, 'ui', 'internal/builder/components/orderlist/{i18n.go,orders_widget.jet}', 1, now(), now()),
('site.component.orderList.pagesText', 'zh-CN', '订单页', 200, 'ui', 'internal/builder/components/orderlist/{i18n.go,orders_widget.jet}', 1, now(), now()),
('site.component.orderList.notice', 'en-US', 'Order list unavailable (site project not resolved)', 200, 'ui', 'internal/builder/components/orderlist/{i18n.go,orders_widget.jet}', 1, now(), now()),
('site.component.orderList.notice', 'zh-CN', '订单列表暂不可用（未取到站点工程）', 200, 'ui', 'internal/builder/components/orderlist/{i18n.go,orders_widget.jet}', 1, now(), now()),
('site.component.userForms.scriptHint', 'en-US', '%s form requires JavaScript; you can also', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,user_forms_widget.jet,userforms.go}', 1, now(), now()),
('site.component.userForms.scriptHint', 'zh-CN', '%s表单需要脚本加载；也可以', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,user_forms_widget.jet,userforms.go}', 1, now(), now()),
('site.component.userForms.openFallback', 'en-US', 'open the %s page', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,user_forms_widget.jet,userforms.go}', 1, now(), now()),
('site.component.userForms.openFallback', 'zh-CN', '打开%s页', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,user_forms_widget.jet,userforms.go}', 1, now(), now()),
('site.component.cardStack.dragAria', 'en-US', 'Rotatable card ring (left and right arrow keys also rotate)', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.dragAria', 'zh-CN', '可拖拽旋转的卡片环（左右方向键也可旋转）', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.deckAria', 'en-US', 'Swipeable card stack (left and right arrow keys also switch)', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.deckAria', 'zh-CN', '可滑动切换的卡片堆叠（左右方向键也可切换）', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.zoomAria', 'en-US', 'Zoom this card', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.zoomAria', 'zh-CN', '放大这张卡片', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.zoomCardAria', 'en-US', 'Zoom card %s', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.zoomCardAria', 'zh-CN', '放大第 %s 张卡片', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.prev', 'en-US', 'Previous', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.prev', 'zh-CN', '上一页', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.next', 'en-US', 'Next', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.next', 'zh-CN', '下一页', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.closeZoomAria', 'en-US', 'Close the zoomed card', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.closeZoomAria', 'zh-CN', '关闭放大的卡片', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.close', 'en-US', 'Close', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.cardStack.close', 'zh-CN', '关闭', 200, 'ui', 'internal/builder/components/cardstack/{i18n.go,cardstack.jet}', 1, now(), now()),
('site.component.userForms.title.login', 'en-US', 'Sign in', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.login', 'zh-CN', '登录', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.register', 'en-US', 'Register', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.register', 'zh-CN', '注册', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.forgot', 'en-US', 'Forgot password', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.forgot', 'zh-CN', '找回密码', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.reset', 'en-US', 'Reset password', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.reset', 'zh-CN', '重置密码', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.account', 'en-US', 'My account', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.account', 'zh-CN', '我的账号', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.profile', 'en-US', 'Account details', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.profile', 'zh-CN', '账号资料', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.preference', 'en-US', 'Preferences', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.preference', 'zh-CN', '账号偏好', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.password', 'en-US', 'Change password', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.password', 'zh-CN', '修改密码', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.sessions', 'en-US', 'Signed-in devices', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now()),
('site.component.userForms.title.sessions', 'zh-CN', '登录设备', 200, 'ui', 'internal/builder/components/userforms/{i18n.go,userforms.go}', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
