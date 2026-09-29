-- 464 · BIZ-3 消费侧接入的访客端与客户页文案词条（20 个 key × 2 语言 = 40 行）。
--
-- 范围（本批唯一新增词条的地方）：
--   · site.fragment.membership.*    —— 两个新片段能力（membershipBadge / membershipPanel）的降级文案。
--     四种降级形态各有各的说法：未登录要访客去登录、端口未接入要运维去接线、没给工程是页面作者的
--     配置问题、解析失败可以稍后重试。三者页面上长得一样的话，装配缺陷会被当成「这站要登录」
--     而被忽略很久 —— 这正是要把它们分开的理由。
--     词条取自 internal/module/runtimefragment/enums 的 Membership* 常量（值即 item_key）。
--   · admin.customer_detail.membership.* —— 客户详情页新增的「会员等级」块
--     （internal/templates/admin/user/customer_detail.html），含三条降级说明的本地兜底 key
--     （internal/module/user/inbound/http/customer_membership.go 的 userLabel）。
--
-- 命名与模块归属：访客端词条走既有 site.fragment.* 四段形态；管理端走 admin.* 三段。
-- 本批**不新增权限点、不新增菜单**（片段是公开面能力，客户页只加展示），故无菜单 / 权限 seed。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register_membership_fragment_i18n.go 里
-- **逐条枚举本批 20 个 item_key**（上界封闭，20 × 2 = 40 行），不用 LIKE 前缀、也不用全库总量 ——
-- 前缀判据会在「已有别的批次同前缀行」时计数虚高而静默跳过本批，全库总量判据会在将来新增
-- 同前缀 key 时永远追不平而每次启动重跑（058 / 076 两次真实故障）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.fragment.membership.title', 'zh-CN', '我的会员', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.title', 'en-US', 'My membership', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.guest', 'zh-CN', '登录后可以查看你的会员等级与专属权益。', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.guest', 'en-US', 'Sign in to see your membership tier and its benefits.', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.unavailable', 'zh-CN', '会员信息暂时不可用。', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.unavailable', 'en-US', 'Membership information is unavailable right now.', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.project_missing', 'zh-CN', '这个页面还没指定站点工程，会员信息无法显示。', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.project_missing', 'en-US', 'This page does not specify a site project, so membership information cannot be shown.', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.failed', 'zh-CN', '会员信息暂时读不出来 —— 稍后刷新页面再试。', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.failed', 'en-US', 'Membership information could not be loaded - refresh the page and try again.', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.default_hint', 'zh-CN', '还不是会员：消费累积到门槛后会自动升级。', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.default_hint', 'en-US', 'Not a member yet: tiers upgrade automatically once cumulative spend reaches a threshold.', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.free_shipping', 'zh-CN', '免运费', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.free_shipping', 'en-US', 'Free shipping', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.discount', 'zh-CN', '%d%% 折扣', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.discount', 'en-US', '%d%% off', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.none', 'zh-CN', '这个等级暂无专属权益。', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('site.fragment.membership.none', 'en-US', 'This tier has no benefits configured.', 200, 'ui', 'runtimefragment membership fragments', 1, now(), now()),
('admin.customer_detail.membership.heading', 'zh-CN', '会员等级', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.heading', 'en-US', 'Membership tier', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.hint', 'zh-CN', '等级按「站点工程 + 客户账号」解析：同一个客户在不同工程可以是不同等级。这里只读 —— 指定等级与解除锁定在「会员归属」页。', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.hint', 'en-US', 'Tiers are resolved per site project plus customer account, so the same customer can hold different tiers in different projects. This block is read-only; assigning a tier or unlocking one happens on the Memberships page.', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.tier', 'zh-CN', '当前等级', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.tier', 'en-US', 'Current tier', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.default_tier', 'zh-CN', '默认等级（这个客户还没有会员归属）', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.default_tier', 'en-US', 'Default tier (this customer has no membership assignment yet)', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.benefits', 'zh-CN', '权益', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.benefits', 'en-US', 'Benefits', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.free_shipping', 'zh-CN', '免运费', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.free_shipping', 'en-US', 'Free shipping', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.discount', 'zh-CN', '折扣', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.discount', 'en-US', 'Discount', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.no_benefit', 'zh-CN', '该等级没有配置权益', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.no_benefit', 'en-US', 'No benefits configured for this tier', 200, 'admin', 'internal/templates/admin/user/customer_detail.html', 1, now(), now()),
('admin.customer_detail.membership.unavailable', 'zh-CN', '会员模块尚未接入，这里看不到等级。', 200, 'admin', 'internal/module/user/inbound/http/customer_membership.go', 1, now(), now()),
('admin.customer_detail.membership.unavailable', 'en-US', 'The membership module is not wired up yet, so no tier is shown here.', 200, 'admin', 'internal/module/user/inbound/http/customer_membership.go', 1, now(), now()),
('admin.customer_detail.membership.no_project', 'zh-CN', '还没有站点工程：会员等级按工程计算，选定工程后才能看到。', 200, 'admin', 'internal/module/user/inbound/http/customer_membership.go', 1, now(), now()),
('admin.customer_detail.membership.no_project', 'en-US', 'No site project yet: membership tiers are counted per project, so select one to see it.', 200, 'admin', 'internal/module/user/inbound/http/customer_membership.go', 1, now(), now()),
('admin.customer_detail.membership.failed', 'zh-CN', '会员等级暂时读不出来 —— 客户资料本身不受影响。', 200, 'admin', 'internal/module/user/inbound/http/customer_membership.go', 1, now(), now()),
('admin.customer_detail.membership.failed', 'en-US', 'The membership tier could not be loaded - the customer record itself is unaffected.', 200, 'admin', 'internal/module/user/inbound/http/customer_membership.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
