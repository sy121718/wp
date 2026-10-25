-- 622 · 邮件模板的可视化正文（块树 + 编译产物）。
--
-- 三列的分工：
--   body_json            编辑器的块树（TemplateTree，schema 1）—— 唯一的**编辑源**；
--   body_compiled_html   由 Go 编译器产出的 HTML（确定性，属性顺序固定）；
--   body_compiled_text   由 text 块按文档序抽出的纯文本。
--
-- 为什么保留 body_html / body_text：存量模板（129 seed 的三个注册流程模板）与「代码模式」
-- 都依赖它们，删列会让旧数据与手写模式一起消失。两条路径的优先级在 service 里：
-- **body_json 非空就用编译产物渲染**，否则回落到 body_html（功能等价于今天）。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + 明确的默认值（NOT NULL DEFAULT ''），
-- 重复执行不报错、也不改动已有行。
ALTER TABLE mail_templates ADD COLUMN IF NOT EXISTS body_json JSONB;
ALTER TABLE mail_templates ADD COLUMN IF NOT EXISTS body_compiled_html TEXT NOT NULL DEFAULT '';
ALTER TABLE mail_templates ADD COLUMN IF NOT EXISTS body_compiled_text TEXT NOT NULL DEFAULT '';
