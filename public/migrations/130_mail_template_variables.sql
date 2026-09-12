-- 130 · mail_templates.variables 改 text[]（issue #37）。
--
-- 起因：129 把 variables 写成 jsonb 数组，而 Go 侧类型是 StringArray（按 PG text[] 的 {} 字面量解析），
-- 拿到 JSON 文本 ["name","code"] 解析失败，ListTemplates 直接报错。
-- 根上的一致做法：这个列本来就该是 text[] —— 与 mail_contacts.tags / mail_campaigns.target_tags 同一种语义
--（字符串列表），同一套编解码。

ALTER TABLE mail_templates ALTER COLUMN variables DROP DEFAULT;

ALTER TABLE mail_templates
    ALTER COLUMN variables TYPE text[]
    USING CASE
        WHEN variables IS NULL THEN '{}'::text[]
        -- jsonb 数组 → text[]。USING 里不能写子查询，所以走字符串转换：
        --   ["name", "code"]  →去引号→ [name, code] →去方括号去空格→ name,code → string_to_array → {name,code}
        WHEN jsonb_typeof(variables) = 'array' THEN
            string_to_array(
                replace(trim(both '[]' from replace(variables::text, '"', '')), ' ', ''),
                ','
            )
        ELSE '{}'::text[]
    END;

ALTER TABLE mail_templates ALTER COLUMN variables SET DEFAULT '{}'::text[];
ALTER TABLE mail_templates ALTER COLUMN variables SET NOT NULL;
