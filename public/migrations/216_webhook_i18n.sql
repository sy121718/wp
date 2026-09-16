-- 216 · webhook 模块文案词条（I18N-002 / CQ-010 口径）
--
-- 模块 enums 的常量值改为 i18n key 后，真正的文案落在这里。响应层
-- pkg/response.translate 按请求语言查表；未命中原样返回 key（可见的降级，不是空白）。
--
-- 为什么必须补：pkg/response.ErrorAuto 按「值是 key 形态」判定业务错误。
-- enums 里留中文原文的话，webhook 的全部业务错误会被判成内部错误 ——
-- 返回 500 + 通用文案，而不会让任何测试变红（这就是 CQ-010 的缺口形态，
-- pkg/response/error_auto_test.go 现在逐个校验全仓 enums 的 Err* / Msg* 常量）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
--
-- 注册：public/migrations/register_analytics.go（Seed 216-webhook-i18n）。

INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
    ('webhook.msg.saveSuccess',   'zh-CN', '保存成功',       'webhook', '', 1, 200, now(), now()),
    ('webhook.msg.saveSuccess',   'en-US', 'Saved',          'webhook', '', 1, 200, now(), now()),
    ('webhook.msg.deleteSuccess', 'zh-CN', '删除成功',       'webhook', '', 1, 200, now(), now()),
    ('webhook.msg.deleteSuccess', 'en-US', 'Deleted',        'webhook', '', 1, 200, now(), now()),
    ('webhook.msg.statusSuccess', 'zh-CN', '状态已更新',     'webhook', '', 1, 200, now(), now()),
    ('webhook.msg.statusSuccess', 'en-US', 'Status updated', 'webhook', '', 1, 200, now(), now()),
    ('webhook.msg.retryQueued',   'zh-CN', '已重新入队',     'webhook', '', 1, 200, now(), now()),
    ('webhook.msg.retryQueued',   'en-US', 'Requeued',       'webhook', '', 1, 200, now(), now()),

    ('webhook.err.urlMalformed',         'zh-CN', '目标地址格式不正确',             'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlMalformed',         'en-US', 'Malformed target URL',           'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlSchemeUnsupported', 'zh-CN', '目标地址只支持 http 或 https',   'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlSchemeUnsupported', 'en-US', 'Only http and https target URLs are supported', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlHostMissing',       'zh-CN', '目标地址缺少主机名',             'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlHostMissing',       'en-US', 'Target URL is missing a host',   'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlUnresolvable',      'zh-CN', '目标地址的域名无法解析',         'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlUnresolvable',      'en-US', 'Target host cannot be resolved', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlDenied',            'zh-CN', '目标地址指向内网或保留地址，已拒绝', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.urlDenied',            'en-US', 'Target resolves to a private or reserved address', 'webhook', '', 1, 200, now(), now()),

    ('webhook.err.invalidParam',       'zh-CN', '参数错误',               'webhook', '', 1, 200, now(), now()),
    ('webhook.err.invalidParam',       'en-US', 'Invalid parameter',      'webhook', '', 1, 200, now(), now()),
    ('webhook.err.endpointNotFound',   'zh-CN', '端点不存在',             'webhook', '', 1, 200, now(), now()),
    ('webhook.err.endpointNotFound',   'en-US', 'Endpoint not found',     'webhook', '', 1, 200, now(), now()),
    ('webhook.err.eventTypeRequired',  'zh-CN', '事件类型不能为空',       'webhook', '', 1, 200, now(), now()),
    ('webhook.err.eventTypeRequired',  'en-US', 'Event type is required', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.targetUrlRequired',  'zh-CN', '目标地址不能为空',       'webhook', '', 1, 200, now(), now()),
    ('webhook.err.targetUrlRequired',  'en-US', 'Target URL is required', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.secretRequired',     'zh-CN', '签名密钥不能为空',       'webhook', '', 1, 200, now(), now()),
    ('webhook.err.secretRequired',     'en-US', 'Signing secret is required', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.deliveryNotFound',   'zh-CN', '投递记录不存在',         'webhook', '', 1, 200, now(), now()),
    ('webhook.err.deliveryNotFound',   'en-US', 'Delivery not found',     'webhook', '', 1, 200, now(), now()),
    ('webhook.err.deliveryNotFailed',  'zh-CN', '只有失败的投递才能重投', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.deliveryNotFailed',  'en-US', 'Only failed deliveries can be retried', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.deliveryNotPending', 'zh-CN', '投递已在进行中或已完成', 'webhook', '', 1, 200, now(), now()),
    ('webhook.err.deliveryNotPending', 'en-US', 'Delivery is already queued or delivered', 'webhook', '', 1, 200, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
