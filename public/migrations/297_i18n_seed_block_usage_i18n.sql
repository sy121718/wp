-- 297 · block 删除保护（ARCH-02）的引用类别词条：拒绝删除时要说清「是哪一类引用在挡路」。
--
-- 背景：BlockReferenceChecker 原先只查主题与页面，且拒绝时只给一句通用的
-- ErrBlockInUse —— 补齐自动实例 / 模板 / 其它块之后，命中的实体数量与种类都变多了，
-- 提示若不说明类别与实体，操作者只能从「删不掉」出发自己把整站文档翻一遍，
-- 而块引用恰好散在 JSONB 的任意深度。
--
-- 六个类别对应 blockcontract.BlockUsageKind 的六个取值（真源在
-- internal/module/block/contract/block_usage.go）：
--   page_document / page_structure / page_revision / theme_slot /
--   block_document / content_template / presentation_instance。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源，
--   运营在后台改过的词条不会被下一次迁移覆盖。
-- 判定见 register_block_usage_i18n.go：枚举本批全部 6 个 key，不数全库行数
--   （058 那种「全库 zh-CN 计数」在存量库永远判定已灌满，新词条不会被灌进去）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('MsgBlockUsagePageDocument', 'zh-CN', '页面文档树引用', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsagePageDocument', 'en-US', 'referenced by a page document tree', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsagePageStructure', 'zh-CN', '页面页眉/页脚/槽位绑定', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsagePageStructure', 'en-US', 'bound as a page header/footer/slot', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsagePageRevision', 'zh-CN', '页面历史修订引用', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsagePageRevision', 'en-US', 'referenced by a page revision', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsageThemeSlot', 'zh-CN', '主题页眉/页脚槽位绑定', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsageThemeSlot', 'en-US', 'bound as a theme header/footer slot', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsageBlockDocument', 'zh-CN', '其它全局块的文档树引用', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsageBlockDocument', 'en-US', 'referenced by another block document tree', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsageContentTemplate', 'zh-CN', '内容模板引用', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsageContentTemplate', 'en-US', 'referenced by a content template', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsagePresentationInstance', 'zh-CN', '自动发布实例引用', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now()),
    ('MsgBlockUsagePresentationInstance', 'en-US', 'referenced by an automatic publishing instance', 200, 'block', 'internal/module/block/enums/block_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
