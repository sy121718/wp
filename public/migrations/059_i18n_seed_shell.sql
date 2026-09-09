-- 059 · i18n 词条 seed（后台外壳 shell.*，多语言 P1 第二步）
--
-- 覆盖：25 个 key / zh-CN 25 行 / en-US 25 行（后台外壳文案，中英均为人工编写，非机器伪造）。
-- 来源：internal/templates/admin/{layout,login}.html、partials/{sidebar,nav-nodes,pagination}.html、
--       internal/module/dashboard/inbound/http/pagination.go（Go 侧拼接的分页文案）。
-- 命名：shell.<区域>.<名称>，与既有 route.* 命名空间并列；category 统一 ui。
-- 占位符：仅 %s（与 pkg/i18n.HasStringPlaceholdersOnly 约定一致，数字在 Go/JS 侧先转字符串）。
-- 兜底：模板层缺词条时回退模板内中文原文（templates.TranslateFunc），不报错。
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE（可重复执行）；
--       注册见 register.go，ConditionSQL 以 shell.* 的 zh-CN 行数 25 为门槛。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('shell.brand', 'en-US', 'Admin Console', 200, 'ui', 'internal/templates/admin/layout.html', 1, now(), now()),
('shell.brand', 'zh-CN', '管理后台', 200, 'ui', 'internal/templates/admin/layout.html', 1, now(), now()),
('shell.action.close', 'en-US', 'Close', 200, 'ui', 'internal/templates/admin/layout.html', 1, now(), now()),
('shell.action.close', 'zh-CN', '关闭', 200, 'ui', 'internal/templates/admin/layout.html', 1, now(), now()),
('shell.action.toggle_theme', 'en-US', 'Toggle theme', 200, 'ui', 'internal/templates/admin/layout.html', 1, now(), now()),
('shell.action.toggle_theme', 'zh-CN', '切换主题', 200, 'ui', 'internal/templates/admin/layout.html', 1, now(), now()),
('shell.lang.label', 'en-US', 'Language', 200, 'ui', 'internal/templates/admin/layout.html', 1, now(), now()),
('shell.lang.label', 'zh-CN', '语言', 200, 'ui', 'internal/templates/admin/layout.html', 1, now(), now()),
('shell.nav.toggle_children', 'en-US', 'Expand / collapse submenu', 200, 'ui', 'internal/templates/admin/partials/nav-nodes.html', 1, now(), now()),
('shell.nav.toggle_children', 'zh-CN', '展开 / 收起子菜单', 200, 'ui', 'internal/templates/admin/partials/nav-nodes.html', 1, now(), now()),
('shell.pagination.info', 'en-US', '%s items, showing %s-%s', 200, 'ui', 'internal/module/dashboard/inbound/http/pagination.go', 1, now(), now()),
('shell.pagination.info', 'zh-CN', '共 %s 条，第 %s-%s 条', 200, 'ui', 'internal/module/dashboard/inbound/http/pagination.go', 1, now(), now()),
('shell.pagination.label', 'en-US', 'Pagination', 200, 'ui', 'internal/templates/admin/partials/pagination.html', 1, now(), now()),
('shell.pagination.label', 'zh-CN', '分页', 200, 'ui', 'internal/templates/admin/partials/pagination.html', 1, now(), now()),
('shell.pagination.next', 'en-US', 'Next', 200, 'ui', 'internal/module/dashboard/inbound/http/pagination.go', 1, now(), now()),
('shell.pagination.next', 'zh-CN', '下一页', 200, 'ui', 'internal/module/dashboard/inbound/http/pagination.go', 1, now(), now()),
('shell.pagination.prev', 'en-US', 'Previous', 200, 'ui', 'internal/module/dashboard/inbound/http/pagination.go', 1, now(), now()),
('shell.pagination.prev', 'zh-CN', '上一页', 200, 'ui', 'internal/module/dashboard/inbound/http/pagination.go', 1, now(), now()),
('shell.sidebar.pin', 'en-US', 'Pin sidebar', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.pin', 'zh-CN', '固定侧栏', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.pin_title', 'en-US', 'Keep the sidebar open after navigating', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.pin_title', 'zh-CN', '固定后导航不再自动收起', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.pinned', 'en-US', 'Pinned', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.pinned', 'zh-CN', '已固定', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.pinned_hint', 'en-US', 'Stays open after navigating', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.pinned_hint', 'zh-CN', '导航后保持展开', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.unpinned_hint', 'en-US', 'Collapses after clicking a menu', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.sidebar.unpinned_hint', 'zh-CN', '点击菜单后收起', 200, 'ui', 'internal/templates/admin/partials/sidebar.html', 1, now(), now()),
('shell.login.captcha', 'en-US', 'Captcha', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha', 'zh-CN', '验证码', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha_busy', 'en-US', 'Too many captcha refreshes, please try again later', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha_busy', 'zh-CN', '验证码刷新过于频繁，请稍后再试', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha_failed', 'en-US', 'Failed to load captcha, please retry', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha_failed', 'zh-CN', '验证码加载失败，请重试', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha_refresh', 'en-US', 'Click to refresh', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha_refresh', 'zh-CN', '点击刷新', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha_ttl', 'en-US', 'Click to refresh (valid for %s seconds)', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.captcha_ttl', 'zh-CN', '点击刷新（%s 秒内有效）', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.failed', 'en-US', 'Sign-in failed', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.failed', 'zh-CN', '登录失败', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.network_error', 'en-US', 'Network error, please retry', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.network_error', 'zh-CN', '网络异常，请重试', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.password', 'en-US', 'Password', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.password', 'zh-CN', '密码', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.submit', 'en-US', 'Sign in', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.submit', 'zh-CN', '登 录', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.title', 'en-US', 'Sign in', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.title', 'zh-CN', '登录', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.username', 'en-US', 'Username', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now()),
('shell.login.username', 'zh-CN', '用户名', 200, 'ui', 'internal/templates/admin/login.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO UPDATE SET
    item_value  = EXCLUDED.item_value,
    http_code   = EXCLUDED.http_code,
    category    = EXCLUDED.category,
    remark      = EXCLUDED.remark,
    status      = EXCLUDED.status,
    update_time = now();
