-- 568 · 全局 AI 悬浮球的词条（docs/17 §1.1 第 3 项）。
--
-- 背景：每个后台页面右下角一个悬浮球，点开在当前页面上问一句，回答出现在旁边。
--   页面上下文只注入 path + query + 页面标题（D9）——不注入 DOM、不注入页面数据，
--   因为列表页每一行都是别人的数据。
--
-- 覆盖：12 个 key × 2 语言 = 24 条。**两个命名空间**：
--   · `admin.ai.fab.*`（5 个）—— 模板消费，走 shell 的 i18n 注入；
--   · `ai.fab.*`（7 个）—— handler 消费，key 由 enums 的 MsgFab* 常量给出。
--   后者之所以也要 seed：门禁 check-i18n-keys-seeded.sh 会扫 enums 里的 key 常量，
--   而它扫到未 seed 的 key 就判红 —— 这与「文案能不能跟着语言走」是两件事。
--   实际上 handler 侧读的是 enums 的中文兜底（悬浮球的回答片段由 c.HTML 单独渲染、
--   不经 shell.Prepare，拿不到 `t` 函数），所以那 7 条 seed 了也暂时用不上；
--   将来把片段渲染也接上 i18n 注入，它们就能直接生效。
--
-- 判据只枚举**模板侧那两个代表 key**（不放 handler 侧的），原因见 register 文件：
--   判据里混进 handler 侧的 key，一旦将来只改那一侧，判据会先失效再引发重复插入。
--
-- 「当前页面」四个字不能省：它告诉用户 AI 知道自己在哪一页，
--   而 D9 明确要求模型自述它的理解 —— 用户据此判断回答是否用错了页面。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.fab.toggle', 'en-US', 'Ask AI', 200, 'admin', 'partials/ai_fab.html: 悬浮球按钮的无障碍名与提示', 1, now(), now()),
('admin.ai.fab.toggle', 'zh-CN', '问 AI', 200, 'admin', 'partials/ai_fab.html: 悬浮球按钮的无障碍名与提示', 1, now(), now()),
('admin.ai.fab.title', 'en-US', 'Ask AI (this page)', 200, 'admin', 'partials/ai_fab.html: 面板标题；「当前页面」要留着——它说明了上下文来源', 1, now(), now()),
('admin.ai.fab.title', 'zh-CN', '问 AI（当前页面）', 200, 'admin', 'partials/ai_fab.html: 面板标题；「当前页面」要留着——它说明了上下文来源', 1, now(), now()),
('admin.ai.fab.history', 'en-US', 'All sessions', 200, 'admin', 'partials/ai_fab.html: 面板右上角去会话页的链接', 1, now(), now()),
('admin.ai.fab.history', 'zh-CN', '全部会话', 200, 'admin', 'partials/ai_fab.html: 面板右上角去会话页的链接', 1, now(), now()),
('admin.ai.fab.placeholder', 'en-US', 'Ask something… (Enter to send, Shift+Enter for a new line)', 200, 'admin', 'partials/ai_fab.html: 输入框占位与无障碍名', 1, now(), now()),
('admin.ai.fab.placeholder', 'zh-CN', '问点什么…（Enter 发送，Shift+Enter 换行）', 200, 'admin', 'partials/ai_fab.html: 输入框占位与无障碍名', 1, now(), now()),
('admin.ai.fab.send', 'en-US', 'Send', 200, 'admin', 'partials/ai_fab.html: 提交按钮', 1, now(), now()),
('admin.ai.fab.send', 'zh-CN', '发送', 200, 'admin', 'partials/ai_fab.html: 提交按钮', 1, now(), now()),
('ai.fab.noModel', 'en-US', 'No model available yet. Configure a provider and API key in LLM management first.', 200, 'admin', 'partials/ai_fab.html: 悬浮球：没有可用模型（异常路径，handler 侧）', 1, now(), now()),
('ai.fab.noModel', 'zh-CN', '还没有可用的模型。请先在「大模型管理」里配置供应商与 API 密钥。', 200, 'admin', 'partials/ai_fab.html: 悬浮球：没有可用模型（异常路径，handler 侧）', 1, now(), now()),
('ai.fab.emptyInput', 'en-US', 'Nothing written yet.', 200, 'admin', 'partials/ai_fab.html: 悬浮球：空输入（异常路径，handler 侧）', 1, now(), now()),
('ai.fab.emptyInput', 'zh-CN', '还没写问题。', 200, 'admin', 'partials/ai_fab.html: 悬浮球：空输入（异常路径，handler 侧）', 1, now(), now()),
('ai.fab.failed', 'en-US', 'This question did not go through. Try again later, or open All sessions for details.', 200, 'admin', 'partials/ai_fab.html: 悬浮球：这一轮失败（异常路径，handler 侧）', 1, now(), now()),
('ai.fab.failed', 'zh-CN', '这次提问没有成功，请稍后再试，或到「全部会话」里看详情。', 200, 'admin', 'partials/ai_fab.html: 悬浮球：这一轮失败（异常路径，handler 侧）', 1, now(), now()),
('ai.fab.truncated', 'en-US', 'The question was too long and has been cut to the first 4000 characters.', 200, 'admin', 'partials/ai_fab.html: 悬浮球：输入被截断（handler 侧）', 1, now(), now()),
('ai.fab.truncated', 'zh-CN', '问题太长，已按前 4000 字截断。', 200, 'admin', 'partials/ai_fab.html: 悬浮球：输入被截断（handler 侧）', 1, now(), now()),
('ai.fab.tools', 'en-US', 'This turn queried data ', 200, 'admin', 'partials/ai_fab.html: 悬浮球：工具次数前缀，后接数字（handler 侧）', 1, now(), now()),
('ai.fab.tools', 'zh-CN', '这轮查了数据', 200, 'admin', 'partials/ai_fab.html: 悬浮球：工具次数前缀，后接数字（handler 侧）', 1, now(), now()),
('ai.fab.toolsUnit', 'en-US', ' times.', 200, 'admin', 'partials/ai_fab.html: 悬浮球：工具次数后缀，接在数字后（handler 侧）', 1, now(), now()),
('ai.fab.toolsUnit', 'zh-CN', ' 次。', 200, 'admin', 'partials/ai_fab.html: 悬浮球：工具次数后缀，接在数字后（handler 侧）', 1, now(), now()),
('ai.fab.empty', 'en-US', 'No answer came back. Open All sessions for this turn''s details.', 200, 'admin', 'partials/ai_fab.html: 悬浮球：既无回答也无工具事件（handler 侧）', 1, now(), now()),
('ai.fab.empty', 'zh-CN', '没有拿到回答。可以到「全部会话」里看这一轮的详情。', 200, 'admin', 'partials/ai_fab.html: 悬浮球：既无回答也无工具事件（handler 侧）', 1, now(), now());
