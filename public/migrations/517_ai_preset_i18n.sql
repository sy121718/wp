-- 517 · AI 供应商页面「第三方模型提供商 / 自定义模型 API」双 tab 与选模型弹窗的词条。
--
-- 背景：本次把「添加供应商」拆成两个 tab（内置预设 / 自定义端点），并把「获取可用模型」
--   改成「先拉候选、勾选后再落库」的弹窗；新增的界面文案与两条服务端响应 key
--   （ai_msg_presets.go 的 MsgModelsAppended / ErrNoModelSelected）都需要词条。
--   判据见 scripts/check-i18n-keys-seeded.sh（只认 INSERT 元组，注释与 ConditionSQL 不算）。
--
-- 覆盖两块（同一批、同一张表，故合成一条迁移）：
--   1. admin.ai.*  23 条：providers.html 的双 tab 表单 + provider_picker.html 弹窗；
--   2. ai.msg.* / ai.err.*  2 条：勾选追加的结果与「一个都没勾」的提示。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不改任何既有词条。
-- 判定写法：ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- —— 1. 双 tab 与选模型弹窗（admin/ai/providers.html、admin/ai/provider_picker.html）——
('admin.ai.tab.preset', 'zh-CN', '第三方模型提供商', 200, 'admin', 'admin/ai/providers.html: 添加区页签', 1, now(), now()),
('admin.ai.tab.preset', 'en-US', 'Third-party model providers', 200, 'admin', 'admin/ai/providers.html: add tabs', 1, now(), now()),
('admin.ai.tab.custom', 'zh-CN', '自定义模型 API', 200, 'admin', 'admin/ai/providers.html: 添加区页签', 1, now(), now()),
('admin.ai.tab.custom', 'en-US', 'Custom model API', 200, 'admin', 'admin/ai/providers.html: add tabs', 1, now(), now()),
('admin.ai.tab.presetHint', 'zh-CN', '从内置目录中选择 OpenAI、Anthropic、Kimi 等提供商，填入其 API 密钥即可使用。', 200, 'admin', 'admin/ai/providers.html: 预设 tab 说明', 1, now(), now()),
('admin.ai.tab.presetHint', 'en-US', 'Pick a provider from the built-in catalog — OpenAI, Anthropic, Kimi and others — and just fill in its API key.', 200, 'admin', 'admin/ai/providers.html: preset tab lead', 1, now(), now()),
('admin.ai.tab.customHint', 'zh-CN', '连接中转站、自部署服务或其他兼容 OpenAI / Anthropic 协议的接口，需填写 API 地址、协议和模型。', 200, 'admin', 'admin/ai/providers.html: 自定义 tab 说明', 1, now(), now()),
('admin.ai.tab.customHint', 'en-US', 'Connect a relay, a self-hosted service, or any other OpenAI / Anthropic compatible endpoint: the API address, protocol and models are yours to fill in.', 200, 'admin', 'admin/ai/providers.html: custom tab lead', 1, now(), now()),
('admin.ai.field.preset', 'zh-CN', '提供商', 200, 'admin', 'admin/ai/providers.html: 字段标签', 1, now(), now()),
('admin.ai.field.preset', 'en-US', 'Provider', 200, 'admin', 'admin/ai/providers.html: field label', 1, now(), now()),
('admin.ai.ph.preset', 'zh-CN', '选择提供商', 200, 'admin', 'admin/ai/providers.html: 下拉首项', 1, now(), now()),
('admin.ai.ph.preset', 'en-US', 'Select a provider', 200, 'admin', 'admin/ai/providers.html: select placeholder', 1, now(), now()),
('admin.ai.hint.preset', 'zh-CN', '标识决定「恢复默认模型」读哪份内置清单。默认地址与协议按该提供商的预设填入，可在下面改。', 200, 'admin', 'admin/ai/providers.html: 预设选择提示', 1, now(), now()),
('admin.ai.hint.preset', 'en-US', 'The key decides which built-in catalog “Restore default models” reads. Base address and protocol are prefilled from that provider’s preset and can be changed below.', 200, 'admin', 'admin/ai/providers.html: preset select hint', 1, now(), now()),
('admin.ai.ph.displayNamePreset', 'zh-CN', '留空使用提供商默认名称', 200, 'admin', 'admin/ai/providers.html: 显示名占位符', 1, now(), now()),
('admin.ai.ph.displayNamePreset', 'en-US', 'Leave blank to use the provider name', 200, 'admin', 'admin/ai/providers.html: display name placeholder', 1, now(), now()),
('admin.ai.ph.protocolPreset', 'zh-CN', '按提供商预设', 200, 'admin', 'admin/ai/providers.html: 协议下拉首项', 1, now(), now()),
('admin.ai.ph.protocolPreset', 'en-US', 'Use the provider preset', 200, 'admin', 'admin/ai/providers.html: protocol placeholder', 1, now(), now()),
('admin.ai.create.submitPreset', 'zh-CN', '添加提供商', 200, 'admin', 'admin/ai/providers.html: 预设 tab 提交', 1, now(), now()),
('admin.ai.create.submitPreset', 'en-US', 'Add provider', 200, 'admin', 'admin/ai/providers.html: preset tab submit', 1, now(), now()),
('admin.ai.create.submitCustom', 'zh-CN', '添加模型 API', 200, 'admin', 'admin/ai/providers.html: 自定义 tab 提交', 1, now(), now()),
('admin.ai.create.submitCustom', 'en-US', 'Add model API', 200, 'admin', 'admin/ai/providers.html: custom tab submit', 1, now(), now()),
('admin.ai.ph.providerKeyCustom', 'zh-CN', '如 acme-gateway', 200, 'admin', 'admin/ai/providers.html: 供应商标识占位符', 1, now(), now()),
('admin.ai.ph.providerKeyCustom', 'en-US', 'e.g. acme-gateway', 200, 'admin', 'admin/ai/providers.html: provider key placeholder', 1, now(), now()),
('admin.ai.hint.providerKeyCustom', 'zh-CN', '以小写字母开头的标识，在请求中唯一标识该提供商，并用于派生凭据名。保存后不可修改，也不参与「恢复默认模型」（自定义端点没有内置清单）。', 200, 'admin', 'admin/ai/providers.html: 供应商标识提示', 1, now(), now()),
('admin.ai.hint.providerKeyCustom', 'en-US', 'A lowercase-leading identifier that uniquely names this provider in requests and derives its credential name. It cannot be changed after saving, and it does not take part in “Restore default models” (custom endpoints have no built-in catalog).', 200, 'admin', 'admin/ai/providers.html: provider key hint', 1, now(), now()),
('admin.ai.ph.baseUrlCustom', 'zh-CN', '如 https://gateway.example.com/v1', 200, 'admin', 'admin/ai/providers.html: API 地址占位符', 1, now(), now()),
('admin.ai.ph.baseUrlCustom', 'en-US', 'e.g. https://gateway.example.com/v1', 200, 'admin', 'admin/ai/providers.html: base URL placeholder', 1, now(), now()),
('admin.ai.picker.title', 'zh-CN', '选择要添加的模型', 200, 'admin', 'admin/ai/provider_picker.html: 弹窗标题', 1, now(), now()),
('admin.ai.picker.title', 'en-US', 'Choose models to add', 200, 'admin', 'admin/ai/provider_picker.html: modal title', 1, now(), now()),
('admin.ai.picker.close', 'zh-CN', '关闭', 200, 'admin', 'admin/ai/provider_picker.html: 关闭按钮', 1, now(), now()),
('admin.ai.picker.close', 'en-US', 'Close', 200, 'admin', 'admin/ai/provider_picker.html: close button', 1, now(), now()),
('admin.ai.picker.lead', 'zh-CN', '以下是模型提供商的可用模型，勾选要添加的模型。', 200, 'admin', 'admin/ai/provider_picker.html: 弹窗导语', 1, now(), now()),
('admin.ai.picker.lead', 'en-US', 'These are the models the provider offers. Tick the ones you want to add.', 200, 'admin', 'admin/ai/provider_picker.html: modal lead', 1, now(), now()),
('admin.ai.picker.search', 'zh-CN', '搜索模型', 200, 'admin', 'admin/ai/provider_picker.html: 搜索框占位符', 1, now(), now()),
('admin.ai.picker.search', 'en-US', 'Search models', 200, 'admin', 'admin/ai/provider_picker.html: search placeholder', 1, now(), now()),
('admin.ai.picker.selectAll', 'zh-CN', '全选', 200, 'admin', 'admin/ai/provider_picker.html: 全选', 1, now(), now()),
('admin.ai.picker.selectAll', 'en-US', 'Select all', 200, 'admin', 'admin/ai/provider_picker.html: select all', 1, now(), now()),
('admin.ai.picker.empty', 'zh-CN', '该提供商没有返回可添加的模型。', 200, 'admin', 'admin/ai/provider_picker.html: 候选为空', 1, now(), now()),
('admin.ai.picker.empty', 'en-US', 'The provider returned no model that can be added.', 200, 'admin', 'admin/ai/provider_picker.html: empty candidates', 1, now(), now()),
('admin.ai.picker.filterEmpty', 'zh-CN', '没有匹配的模型。', 200, 'admin', 'admin/ai/provider_picker.html: 筛选无结果', 1, now(), now()),
('admin.ai.picker.filterEmpty', 'en-US', 'No model matches.', 200, 'admin', 'admin/ai/provider_picker.html: filtered empty', 1, now(), now()),
('admin.ai.picker.inCatalog', 'zh-CN', '已在目录', 200, 'admin', 'admin/ai/provider_picker.html: 已在目录标记', 1, now(), now()),
('admin.ai.picker.inCatalog', 'en-US', 'Already in catalog', 200, 'admin', 'admin/ai/provider_picker.html: already in catalog badge', 1, now(), now()),
('admin.ai.picker.submit', 'zh-CN', '添加所选', 200, 'admin', 'admin/ai/provider_picker.html: 弹窗提交', 1, now(), now()),
('admin.ai.picker.submit', 'en-US', 'Add selected', 200, 'admin', 'admin/ai/provider_picker.html: modal submit', 1, now(), now()),
-- —— 2. 服务端响应（internal/module/ai/enums/ai_msg_presets.go）——
('ai.msg.modelsAppended', 'zh-CN', '已添加 {n} 个模型。', 200, 'ai', 'ai_msg_presets.go: 勾选追加结果', 1, now(), now()),
('ai.msg.modelsAppended', 'en-US', 'Added {n} model(s).', 200, 'ai', 'ai_msg_presets.go: append result', 1, now(), now()),
('ai.err.noModelSelected', 'zh-CN', '请先勾选要添加的模型。', 400, 'ai', 'ai_msg_presets.go: 未勾选或勾中的都已在目录', 1, now(), now()),
('ai.err.noModelSelected', 'en-US', 'Select at least one model to add.', 400, 'ai', 'ai_msg_presets.go: nothing selected (or all already present)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
