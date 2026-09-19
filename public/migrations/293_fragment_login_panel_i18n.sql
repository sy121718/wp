-- 293 · i18n 词条 seed（运行时片段登录面板文案 site.fragment.login_panel.*，I18N-011）
--
-- 背景：/_fragments 端点的语言链路（?lang → project_locales 校验 → 工程默认语言）早已落地，
-- 但 loginPanel 的两句文案一直硬编码在 Go 里（runtimefragment/capability.go），
-- 多语言站点上它是唯一恒为默认语言的片段文案。这里补上词条、代码侧接入 r.tr 链路，
-- 与 156/157/158 的 site.fragment.* 同一台账口径（取词失败回退 Go 里的中文原文）。
--
-- 命名：site.fragment.login_panel.<语义>。
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE（与 156 同形）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.fragment.login_panel.login_register', 'zh-CN', '登录 / 注册', 200, 'ui', 'runtimefragment/capability.go', 1, now(), now()),
('site.fragment.login_panel.login_register', 'en-US', 'Sign in / Sign up', 200, 'ui', 'runtimefragment/capability.go', 1, now(), now()),
('site.fragment.login_panel.continue_shopping', 'zh-CN', '继续购物', 200, 'ui', 'runtimefragment/capability.go', 1, now(), now()),
('site.fragment.login_panel.continue_shopping', 'en-US', 'Continue shopping', 200, 'ui', 'runtimefragment/capability.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO UPDATE SET
  item_value = EXCLUDED.item_value,
  remark = EXCLUDED.remark,
  update_time = now();