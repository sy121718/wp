-- 465 · 站点级运费设置的文案词条（9 个 key × 2 语言 = 18 行）。
--
-- 范围（本批唯一新增词条的地方）：
--   · ErrShippingBaseFeeInvalid / ErrShippingFreeThresholdInvalid —— 站点设置页保存运费时的
--     两条就地提示（internal/module/project/enums/project_enums.go）。两个字段各一个 key：
--     这一页有十几个输入框，不指明是哪一个的提示等于让用户自己猜。
--     出口是 ?err= 通道（303 回带一句受控文案），所以它们同时被登记进
--     projectPageErrKeys 白名单（读侧只认这批 key 的译文）。
--   · admin.settings.* —— 站点设置页新增的「运费」分组（internal/templates/admin/project/settings.html）
--     的静态文案位（小节标题 / 两个字段标签与占位符 / 两条说明）。中文兜底留在调用点，
--     词条缺失时页面显示中文而不是裸 key。
--
-- 命名与模块归属：错误 key 沿用 `ErrXxx = "ErrXxx"` 的老式形态（与 403 里的
-- ErrGA4IDInvalid / ErrGSCVerificationInvalid 同族，category = project）；
-- 页面文案位走既有 `admin.settings.*` 点分形态（category = admin）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register_shipping_settings_i18n.go 里
-- **逐条枚举本批 9 个 item_key**（上界封闭，9 × 2 = 18 行），不用 LIKE 前缀（别的批次已有
-- admin.settings.* 前缀行 → 计数虚高 → 本批被静默跳过，058 的真实故障），也不用全库总量
--（将来新增同前缀 key 会永远追不平 → 每次启动都重跑，076 的真实故障）。
--
-- 本批**不新增权限点、不新增菜单、不新增路由**：设置保存复用既有的
-- POST /admin/settings/save 与它的权限点。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrShippingBaseFeeInvalid', 'zh-CN', '基础运费必须是不小于 0 的数字（单位：元，最多两位小数），且不超过 10000 元', 400, 'project', 'project/inbound/http: 站点设置页保存基础运费时校验（与读取侧同一判据）', 1, now(), now()),
('ErrShippingBaseFeeInvalid', 'en-US', 'The base shipping fee must be a number of at least 0 (in yuan, up to 2 decimals) and no more than 10000', 400, 'project', 'project/inbound/http: 站点设置页保存基础运费时校验（与读取侧同一判据）', 1, now(), now()),
('ErrShippingFreeThresholdInvalid', 'zh-CN', '满额免运费门槛必须是不小于 0 的数字（单位：元，最多两位小数），且不超过 10000 元；留空或 0 表示不启用', 400, 'project', 'project/inbound/http: 站点设置页保存免运费门槛时校验（与读取侧同一判据）', 1, now(), now()),
('ErrShippingFreeThresholdInvalid', 'en-US', 'The free-shipping threshold must be a number of at least 0 (in yuan, up to 2 decimals) and no more than 10000; leave it empty or 0 to disable', 400, 'project', 'project/inbound/http: 站点设置页保存免运费门槛时校验（与读取侧同一判据）', 1, now(), now()),
('admin.settings.section.shipping', 'zh-CN', '运费', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.section.shipping', 'en-US', 'Shipping', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.field.shipping_base_fee', 'zh-CN', '基础运费（元）', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.field.shipping_base_fee', 'en-US', 'Base shipping fee (yuan)', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.ph.shipping_base_fee', 'zh-CN', '留空或 0 = 不收运费', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.ph.shipping_base_fee', 'en-US', 'Leave empty or 0 to charge nothing', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.shipping', 'zh-CN', '结算时对每单收取的固定运费，单位元（最多两位小数，上限 1 万元）。留空或填 0 = 不收运费。它只影响运费：商品小计仍由订单域现算，不会因为这个值变动。小计达到下面的门槛、或下单客户有免运费会员权益时，这一笔会被免掉。', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.shipping', 'en-US', 'A fixed fee charged per order at checkout, in yuan (up to 2 decimals, max 10000). Leave it empty or 0 to charge nothing. It only affects shipping: the item subtotal is still computed by the order module and never changes because of this value. The fee is waived once the subtotal reaches the threshold below, or when the customer holds a free-shipping membership benefit.', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.field.shipping_free_threshold', 'zh-CN', '满额免运费门槛（元）', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.field.shipping_free_threshold', 'en-US', 'Free shipping threshold (yuan)', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.ph.shipping_free_threshold', 'zh-CN', '留空或 0 = 不启用', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.ph.shipping_free_threshold', 'en-US', 'Leave empty or 0 to disable', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.shipping_threshold', 'zh-CN', '商品小计达到这个金额就免掉上面的基础运费（单位元）。留空或填 0 = 不启用满额免运费。它与基础运费各自独立：填 50 表示买满 50 元免运费，与那笔运费是多少无关。', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now()),
('admin.settings.hint.shipping_threshold', 'en-US', 'Once the item subtotal reaches this amount the base shipping fee above is waived (in yuan). Leave it empty or 0 to disable free shipping by threshold. It is independent of the base fee: 50 means free shipping on orders of 50 yuan or more, regardless of how large that fee is.', 200, 'admin', 'internal/templates/admin/project/settings.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
