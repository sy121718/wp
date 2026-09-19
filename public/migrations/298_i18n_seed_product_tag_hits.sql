-- 298 · i18n 词条 seed（商品标签页「命中商品」展开区，2 key × 2 语言 = 4 行）
--
-- 背景：审计 PERF-02（标签后台 N+1 读取）把命中商品从首屏内联改成展开时按页取
--       （HTMX 片段 /admin/product-tags/hits），新增两处文案位：展开面板的空态提示、
--       片段取数失败的前缀。
-- 为什么必须 seed：模板写的是 t(key, 中文兜底) —— 词条命中显示译文，未命中**回落中文兜底**；
--       缺 en-US 不会有任何报错，只会让英文界面显示中文（与 228/229/230/283 同一类坑）。
-- 覆盖：2 个 key / zh-CN 2 行 / en-US 2 行（英文为人工翻译，不是复制中文）。
-- 来源：internal/templates/admin/product_tags.html（展开面板初始提示）与
--       internal/templates/admin/partials/product_tag_hits.html（空态提示同 key；失败前缀一句
--       在 product_tag_page.go 里经 productErrText 白名单产出，与模板拼接展示）。
-- 注意：hits.error 的英文值**尾部有一个空格** —— 模板把这句前缀与错误正文直接拼接
--       （{{t}} 紧跟 {{.Err}}），不留空格会拼成 products:Tag not found。
-- 语义：ON CONFLICT DO NOTHING —— seed 是**默认值来源**，不是真相来源；
--       运营在后台改过的词条不会被下一次部署静默回滚。
-- 幂等：注册见 register_product_tag_hits_i18n.go，ConditionSQL 以这 2 个 key 的 zh-CN 行数为门槛。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.product_tags.list.hitHint', 'en-US', 'Click the count in the Matched column of the tag list to browse the products this tag matched, one page at a time.', 200, 'ui', 'product: /admin/product-tags 命中商品展开区的空态提示', 1, now(), now()),
('admin.product_tags.list.hitHint', 'zh-CN', '点标签列表里「命中」列的条数，在这里按页查看该标签命中的商品。', 200, 'ui', 'product: /admin/product-tags 命中商品展开区的空态提示', 1, now(), now()),
('admin.product_tags.hits.error', 'en-US', 'Could not load matched products: ', 200, 'ui', 'product: /admin/product-tags 命中商品片段取数失败的前缀', 1, now(), now()),
('admin.product_tags.hits.error', 'zh-CN', '命中商品没能取出来：', 200, 'ui', 'product: /admin/product-tags 命中商品片段取数失败的前缀', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
