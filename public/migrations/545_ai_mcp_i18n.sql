-- 545 · 后台「MCP 与外部访问」页（admin/ai/mcp.html 与 ai/mcp_tokens.html）的全部词条
--
-- 为什么是「全部重列」而不是只补差额：这一页的文案先前只存在于**运行库里**，
--   仓库侧没有任何迁移提供它们（`grep -rn 'admin.ai.mcp' public/migrations/*.sql` 在本次之前是空的）。
--   后果很具体：**新库 / 重建库跑完全部迁移后，这一页所有文案都会退化成模板兜底**——
--   中文站看着像正常（兜底就是中文），英文站整页中文，而且没有任何报错。
--
-- 键的来源（不靠手抄）：模板与 handler 里正则抽 tr("键") 得到全部用例，
--   用 `SELECT ... FROM sys_i18n WHERE item_key LIKE 'admin.ai.mcp.%'` 取现有行的**原值**
--   逐条落进本文件 —— 不重写文案，避免「迁移里的值和线上看到的不一样」。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（同键同语言保留最先到的那条）。
--   改文案请**新增**迁移用 UPDATE；直接改本文件对已执行过的库无效
--   （ConditionSQL 会判为已完成并跳过，AGENTS.md「迁移」一节记着这个坑）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.mcp.endpoint.hint', 'en-US', 'Point your MCP client (HTTP transport) at the URL below and send Authorization: Bearer <token> with each request.', 200, 'admin', 'admin/ai/mcp.html: 接入地址说明', 1, now(), now()),
('admin.ai.mcp.endpoint.hint', 'zh-CN', '把下面这个地址填进 MCP 客户端（HTTP 传输），并在请求头带上 Authorization: Bearer <令牌>。', 200, 'admin', 'admin/ai/mcp.html: 接入地址说明', 1, now(), now()),
('admin.ai.mcp.endpoint.title', 'en-US', 'Endpoint', 200, 'admin', 'admin/ai/mcp.html: 接入地址小节', 1, now(), now()),
('admin.ai.mcp.endpoint.title', 'zh-CN', '接入地址', 200, 'admin', 'admin/ai/mcp.html: 接入地址小节', 1, now(), now()),
('admin.ai.mcp.help.body', 'en-US', 'Expose this site''s tools to external programs. A client connects to POST /mcp with Authorization: Bearer <token>, lists tools and calls them over the MCP protocol. The token plaintext appears only once at creation - save it immediately.', 200, 'admin', 'ai/mcp.html: 说明气泡正文', 1, now(), now()),
('admin.ai.mcp.help.body', 'zh-CN', '把本站的工具开放给外部程序调用。外部程序带一把令牌（Authorization: Bearer）连 POST /mcp，按 MCP 协议先列工具、再调工具。令牌明文只在生成时出现一次，请立刻保存。', 200, 'admin', 'ai/mcp.html: 说明气泡正文', 1, now(), now()),
('admin.ai.mcp.help.label', 'en-US', 'View help', 200, 'admin', 'ai/mcp.html: 页头说明气泡按钮', 1, now(), now()),
('admin.ai.mcp.help.label', 'zh-CN', '查看说明', 200, 'admin', 'ai/mcp.html: 页头说明气泡按钮', 1, now(), now()),
('admin.ai.mcp.help.title', 'en-US', 'What this page does', 200, 'admin', 'ai/mcp.html: 说明气泡标题', 1, now(), now()),
('admin.ai.mcp.help.title', 'zh-CN', '这一页管什么', 200, 'admin', 'ai/mcp.html: 说明气泡标题', 1, now(), now()),
('admin.ai.mcp.lead', 'en-US', 'Let external AI or harnesses call this site''s tools as one of your admin accounts. A token is shown once; its power is the intersection of the scopes you grant and what the account still has.', 200, 'admin', 'admin/ai/mcp.html: 页头说明', 1, now(), now()),
('admin.ai.mcp.lead', 'zh-CN', '让外部 AI / harness 以某个后台账号的身份调用本站工具。令牌只显示一次，权限是「令牌声明的范围」与「账号自身仍有的权限」的交集。', 200, 'admin', 'admin/ai/mcp.html: 页头说明', 1, now(), now()),
('admin.ai.mcp.scope.hint', 'en-US', 'A token can only call tools it declares AND that the account still has permission for — both, not either.', 200, 'admin', 'admin/ai/mcp.html: 权限口径说明', 1, now(), now()),
('admin.ai.mcp.scope.hint', 'zh-CN', '令牌只能调用它声明、且账号自身仍有权限的工具；两者取交集，缺一不可。', 200, 'admin', 'admin/ai/mcp.html: 权限口径说明', 1, now(), now()),
('admin.ai.mcp.title', 'en-US', 'MCP and external access', 200, 'admin', 'admin/ai/mcp.html: 页面标题', 1, now(), now()),
('admin.ai.mcp.title', 'zh-CN', 'MCP 与外部访问', 200, 'admin', 'admin/ai/mcp.html: 页面标题', 1, now(), now()),
('admin.ai.mcp.token.col.actions', 'en-US', 'Actions', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（操作）', 1, now(), now()),
('admin.ai.mcp.token.col.actions', 'zh-CN', '操作', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（操作）', 1, now(), now()),
('admin.ai.mcp.token.col.expires', 'en-US', 'Expires', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（过期）', 1, now(), now()),
('admin.ai.mcp.token.col.expires', 'zh-CN', '过期', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（过期）', 1, now(), now()),
('admin.ai.mcp.token.col.lastUsed', 'en-US', 'Last used', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（最近使用）', 1, now(), now()),
('admin.ai.mcp.token.col.lastUsed', 'zh-CN', '最近使用', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（最近使用）', 1, now(), now()),
('admin.ai.mcp.token.col.name', 'en-US', 'Purpose', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（用途）', 1, now(), now()),
('admin.ai.mcp.token.col.name', 'zh-CN', '用途', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（用途）', 1, now(), now()),
('admin.ai.mcp.token.col.prefix', 'en-US', 'Prefix', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（前缀）', 1, now(), now()),
('admin.ai.mcp.token.col.prefix', 'zh-CN', '令牌前缀', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（前缀）', 1, now(), now()),
('admin.ai.mcp.token.col.scopes', 'en-US', 'Scopes', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（权限点）', 1, now(), now()),
('admin.ai.mcp.token.col.scopes', 'zh-CN', '权限点', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（权限点）', 1, now(), now()),
('admin.ai.mcp.token.col.status', 'en-US', 'Status', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（状态）', 1, now(), now()),
('admin.ai.mcp.token.col.status', 'zh-CN', '状态', 200, 'admin', 'admin/ai/mcp.html: 令牌表头（状态）', 1, now(), now()),
('admin.ai.mcp.token.create.expires', 'en-US', 'Expires (optional, valid through that day)', 200, 'admin', 'admin/ai/mcp.html: 令牌过期日字段', 1, now(), now()),
('admin.ai.mcp.token.create.expires', 'zh-CN', '过期日（可空，当天有效）', 200, 'admin', 'admin/ai/mcp.html: 令牌过期日字段', 1, now(), now()),
('admin.ai.mcp.token.create.name', 'en-US', 'Purpose note', 200, 'admin', 'admin/ai/mcp.html: 令牌用途字段', 1, now(), now()),
('admin.ai.mcp.token.create.name', 'zh-CN', '用途备注', 200, 'admin', 'admin/ai/mcp.html: 令牌用途字段', 1, now(), now()),
('admin.ai.mcp.token.create.name.hint', 'en-US', 'Say who it is for. It is the only way to recognize a token when you later need to revoke it.', 200, 'admin', 'ai/mcp.html: 用途备注提示', 1, now(), now()),
('admin.ai.mcp.token.create.name.hint', 'zh-CN', '写清「给谁用」；日后要撤销时，只能靠这一行认出来。', 200, 'admin', 'ai/mcp.html: 用途备注提示', 1, now(), now()),
('admin.ai.mcp.token.create.name.ph', 'en-US', 'e.g. dsh local client', 200, 'admin', 'ai/mcp.html: 用途备注占位符', 1, now(), now()),
('admin.ai.mcp.token.create.name.ph', 'zh-CN', '例如：dsh 本地客户端', 200, 'admin', 'ai/mcp.html: 用途备注占位符', 1, now(), now()),
('admin.ai.mcp.token.create.noScopes', 'en-US', 'No tools are registered, so there is nothing to grant.', 200, 'admin', 'ai/mcp.html: 无工具可授权', 1, now(), now()),
('admin.ai.mcp.token.create.noScopes', 'zh-CN', '当前没有注册任何工具，因此没有可勾选的权限点。', 200, 'admin', 'ai/mcp.html: 无工具可授权', 1, now(), now()),
('admin.ai.mcp.token.create.scopes', 'en-US', 'Scopes', 200, 'admin', 'admin/ai/mcp.html: 令牌权限点字段', 1, now(), now()),
('admin.ai.mcp.token.create.scopes', 'zh-CN', '权限点', 200, 'admin', 'admin/ai/mcp.html: 令牌权限点字段', 1, now(), now()),
('admin.ai.mcp.token.create.section', 'en-US', 'Create a token', 200, 'admin', 'admin/ai/mcp.html: 生成令牌小节标题', 1, now(), now()),
('admin.ai.mcp.token.create.section', 'zh-CN', '生成新令牌', 200, 'admin', 'admin/ai/mcp.html: 生成令牌小节标题', 1, now(), now()),
('admin.ai.mcp.token.create.submit', 'en-US', 'Create token', 200, 'admin', 'admin/ai/mcp.html: 生成令牌按钮', 1, now(), now()),
('admin.ai.mcp.token.create.submit', 'zh-CN', '生成令牌', 200, 'admin', 'admin/ai/mcp.html: 生成令牌按钮', 1, now(), now()),
('admin.ai.mcp.token.empty', 'en-US', 'No tokens yet. Create one above; revoke it from this table later.', 200, 'admin', 'admin/ai/mcp.html: 令牌空态', 1, now(), now()),
('admin.ai.mcp.token.empty', 'zh-CN', '还没有令牌。上面生成一把，之后在这张表里撤销。', 200, 'admin', 'admin/ai/mcp.html: 令牌空态', 1, now(), now()),
('admin.ai.mcp.token.never', 'en-US', 'Never used', 200, 'admin', 'admin/ai/mcp.html: 未使用占位', 1, now(), now()),
('admin.ai.mcp.token.never', 'zh-CN', '从未使用', 200, 'admin', 'admin/ai/mcp.html: 未使用占位', 1, now(), now()),
('admin.ai.mcp.token.noExpiry', 'en-US', 'No expiry', 200, 'admin', 'admin/ai/mcp.html: 无过期占位', 1, now(), now()),
('admin.ai.mcp.token.noExpiry', 'zh-CN', '不过期', 200, 'admin', 'admin/ai/mcp.html: 无过期占位', 1, now(), now()),
('admin.ai.mcp.token.once.hint', 'en-US', 'Copy it somewhere safe now. Only a hash is stored, so once this panel is gone the token cannot be recovered — you would create a new one.', 200, 'admin', 'admin/ai/mcp.html: 一次性明文提示正文', 1, now(), now()),
('admin.ai.mcp.token.once.hint', 'zh-CN', '现在复制并保存到安全的地方。库里只存哈希，关掉这块之后再也看不到明文，只能重新生成。', 200, 'admin', 'admin/ai/mcp.html: 一次性明文提示正文', 1, now(), now()),
('admin.ai.mcp.token.once.title', 'en-US', 'This token is shown once', 200, 'admin', 'admin/ai/mcp.html: 一次性明文提示标题', 1, now(), now()),
('admin.ai.mcp.token.once.title', 'zh-CN', '令牌只显示这一次', 200, 'admin', 'admin/ai/mcp.html: 一次性明文提示标题', 1, now(), now()),
('admin.ai.mcp.token.revoke', 'en-US', 'Revoke', 200, 'admin', 'admin/ai/mcp.html: 撤销按钮', 1, now(), now()),
('admin.ai.mcp.token.revoke', 'zh-CN', '撤销', 200, 'admin', 'admin/ai/mcp.html: 撤销按钮', 1, now(), now()),
('admin.ai.mcp.token.revokeConfirm', 'en-US', 'Revoke this token? It stops working immediately and cannot be restored.', 200, 'admin', 'admin/ai/mcp.html: 撤销二次确认文案', 1, now(), now()),
('admin.ai.mcp.token.revokeConfirm', 'zh-CN', '确定撤销这把令牌？撤销后立即失效且无法恢复。', 200, 'admin', 'admin/ai/mcp.html: 撤销二次确认文案', 1, now(), now()),
('admin.ai.mcp.token.revokeDone', 'en-US', 'Token revoked (effective immediately)', 200, 'admin', 'ai/mcp.html: 撤销成功回执', 1, now(), now()),
('admin.ai.mcp.token.revokeDone', 'zh-CN', '已撤销该令牌（立即失效）', 200, 'admin', 'ai/mcp.html: 撤销成功回执', 1, now(), now()),
('admin.ai.mcp.token.revokedHint', 'en-US', 'Revoked tokens stay in the list (so the audit can answer when they were revoked) but stop working.', 200, 'admin', 'admin/ai/mcp.html: 已撤销说明', 1, now(), now()),
('admin.ai.mcp.token.revokedHint', 'zh-CN', '已撤销的令牌保留在表里（审计要能回答「什么时候撤的」），但不再可用。', 200, 'admin', 'admin/ai/mcp.html: 已撤销说明', 1, now(), now()),
('admin.ai.mcp.token.section', 'en-US', 'Access tokens', 200, 'admin', 'admin/ai/mcp.html: 令牌小节标题', 1, now(), now()),
('admin.ai.mcp.token.section', 'zh-CN', '访问令牌', 200, 'admin', 'admin/ai/mcp.html: 令牌小节标题', 1, now(), now()),
('admin.ai.mcp.tool.col.desc', 'en-US', 'Description', 200, 'admin', 'admin/ai/mcp.html: 工具说明表头', 1, now(), now()),
('admin.ai.mcp.tool.col.desc', 'zh-CN', '说明', 200, 'admin', 'admin/ai/mcp.html: 工具说明表头', 1, now(), now()),
('admin.ai.mcp.tool.col.name', 'en-US', 'Tool', 200, 'admin', 'admin/ai/mcp.html: 工具表头', 1, now(), now()),
('admin.ai.mcp.tool.col.name', 'zh-CN', '工具', 200, 'admin', 'admin/ai/mcp.html: 工具表头', 1, now(), now()),
('admin.ai.mcp.tool.col.perm', 'en-US', 'Required permission', 200, 'admin', 'admin/ai/mcp.html: 工具权限表头', 1, now(), now()),
('admin.ai.mcp.tool.col.perm', 'zh-CN', '所需权限点', 200, 'admin', 'admin/ai/mcp.html: 工具权限表头', 1, now(), now()),
('admin.ai.mcp.tool.empty', 'en-US', 'No tools are registered (no module registered any tool at assembly time).', 200, 'admin', 'admin/ai/mcp.html: 工具空态', 1, now(), now()),
('admin.ai.mcp.tool.empty', 'zh-CN', '当前没有注册工具（装配期未注册任何模块的工具）。', 200, 'admin', 'admin/ai/mcp.html: 工具空态', 1, now(), now()),
('admin.ai.mcp.tool.hint', 'en-US', 'Tools are registered in code (each module''s inbound/mcp) at assembly time. This page is read-only: there is no runtime way to add or remove one.', 200, 'admin', 'ai/mcp.html: 工具小节说明', 1, now(), now()),
('admin.ai.mcp.tool.hint', 'zh-CN', '工具在代码里注册（各模块的 inbound/mcp），装配期一次性完成 —— 这一页只读，运行期没有增删入口。', 200, 'admin', 'ai/mcp.html: 工具小节说明', 1, now(), now()),
('admin.ai.mcp.tool.section', 'en-US', 'Available tools', 200, 'admin', 'admin/ai/mcp.html: 工具小节标题', 1, now(), now()),
('admin.ai.mcp.tool.section', 'zh-CN', '可用工具', 200, 'admin', 'admin/ai/mcp.html: 工具小节标题', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 回滚（手工）：DELETE FROM sys_i18n WHERE item_key LIKE 'admin.ai.mcp.%';
