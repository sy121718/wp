-- ========================================
-- 409 — 运行时片段端点「错误出口」的受控文案（访问面）
--
-- 背景：internal/module/runtimefragment/endpoint.go 有两处把 Go 的 error 原文写进响应：
--
--   c.String(http.StatusBadRequest, perr.Error())   -- collectFragmentParams 的白名单拒绝
--   c.String(http.StatusBadRequest, err.Error())    -- validateContext 的白名单拒绝
--
-- 本批把两处收口到 fragment_err.go 的统一出口：响应只出本迁移登记的受控文案，
-- 错误原文（以及非法的 context 值）只进结构化日志。
--
-- 为什么这两处必须走 sys_i18n：本端点是**访问面**（/_fragments/{type} 由访客浏览器 /
-- htmx 调用，不走后台鉴权），而访问面是多语言的 —— 片段语言由 ?lang 协商
-- （lang_resolve.go）。硬编码中文会让英文站点上的错误响应弹中文提示，
-- 这正是 293 修过的那类缺陷（loginPanel 两句写死在 Go 里，英文站点一直显示中文）。
--
-- 为什么是 5 个 key 而不是一句通用文案：这四条「请求被拒」的文案对应
-- **消费方要做的不同动作** —— 参数过多要减少参数名、参数非法要缩短某个名或值、
-- 表单解析失败要修请求体、上下文非法要改用枚举内的值。第 5 条
-- （site.fragment.err.internal）是**归口文案**：判定表未命中时用，当前值域封闭走不到它，
-- 留着是因为出口是通用形状（将来把渲染 / 数据源错误接到这里也不会把原文铺在响应上）。
-- 同 project 域的 project_err.go 用 11 个 key 映射 service 哨兵，是同一判据。
--
-- 中文原文同时在 Go 侧作为 fallback（fragment_err.go 的 fragmentErrText）：
-- 词条缺失时回退原文，绝不输出裸 key、绝不输出空串（与 156/157/158/293 同口径）。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**不修改任何既有词条的值**。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.fragment.err.form_parse', 'zh-CN', '片段请求表单无法解析', 400, 'ui', 'runtimefragment/endpoint.go: POST 表单解析失败（客户端请求体格式问题）', 1, now(), now()),
('site.fragment.err.form_parse', 'en-US', 'Could not parse the fragment request form', 400, 'ui', 'runtimefragment/endpoint.go: POST 表单解析失败（客户端请求体格式问题）', 1, now(), now()),
('site.fragment.err.too_many_params', 'zh-CN', '片段请求参数过多', 400, 'ui', 'runtimefragment/endpoint.go: 参数名数量超过 GET/POST 上限', 1, now(), now()),
('site.fragment.err.too_many_params', 'en-US', 'Too many fragment request parameters', 400, 'ui', 'runtimefragment/endpoint.go: 参数名数量超过 GET/POST 上限', 1, now(), now()),
('site.fragment.err.param_invalid', 'zh-CN', '片段请求参数不合法', 400, 'ui', 'runtimefragment/endpoint.go: 参数名超 64 字节或参数值超长度上限', 1, now(), now()),
('site.fragment.err.param_invalid', 'en-US', 'Invalid fragment request parameter', 400, 'ui', 'runtimefragment/endpoint.go: 参数名超 64 字节或参数值超长度上限', 1, now(), now()),
('site.fragment.err.context', 'zh-CN', '片段语义上下文不合法', 400, 'ui', 'runtimefragment/registry.go: 语义上下文不在枚举内（非法值只进日志，不回显）', 1, now(), now()),
('site.fragment.err.context', 'en-US', 'Invalid fragment semantic context', 400, 'ui', 'runtimefragment/registry.go: 语义上下文不在枚举内（非法值只进日志，不回显）', 1, now(), now()),
('site.fragment.err.internal', 'zh-CN', '片段请求处理失败', 500, 'ui', 'runtimefragment/fragment_err.go: 归口文案（判定表未命中时使用）', 1, now(), now()),
('site.fragment.err.internal', 'en-US', 'Fragment request failed', 500, 'ui', 'runtimefragment/fragment_err.go: 归口文案（判定表未命中时使用）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
