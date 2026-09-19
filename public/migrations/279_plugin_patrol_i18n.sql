-- 279 · 插件管理页「产物对账巡检」词条（admin.plugins.patrol.*，12 个 key × 中英 = 24 行）。
--
-- 背景：插件卸载要清三处互相独立的存储 —— L1 schema（PG）、注册行（PG）、存储目录（文件系统）。
-- 后者的清理不参与事务（跨库），所以每一步都可能单独失败，而留下的残片**没有任何入口能发现**：
-- 孤儿 schema / 缺 schema 的注册行 / 孤儿目录 / 目录缺失的注册行。
-- service/plugin_patrol.go 把这四类对账出来，本批把结果呈现在 /admin/plugins 上。
--
-- 巡检**只报告、不自动清理**：DROP SCHEMA ... CASCADE 会连表里的数据一起删，
-- RemoveAll 会连目录里的资产一起删 —— 孤儿里可能有真实业务数据或还没迁走的资产，
-- 「反正没注册」不是丢它的理由。文案里必须说清这件事，否则巡检反而会诱导人去乱删。
--
-- http_code 用 200、category 用 admin：这些是页面标签与说明，不是错误文案
--（与 278 的 admin.pages.receipts.* 同口径）。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.plugins.patrol.orphans_title', 'zh-CN', '产物对账巡检：孤儿 schema', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.orphans_title', 'en-US', 'Artifact reconciliation: orphan schemas', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.orphans_desc', 'zh-CN', '这些 schema 在数据库里还在，但注册表里已经没有对应插件（卸载未清干净，或 schema 是手工建的）。巡检不自动清理：DROP SCHEMA ... CASCADE 会连里面的数据一起删除，请先确认表里有没有需要保留的数据。', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.orphans_desc', 'en-US', 'These schemas still exist in the database, but no matching plugin remains in the registry (an incomplete uninstall, or a schema created by hand). The patrol never cleans up automatically: DROP SCHEMA ... CASCADE deletes the data inside as well, so first confirm whether the tables hold anything worth keeping.', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.col_tables', 'zh-CN', '表数量', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.col_tables', 'en-US', 'Tables', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.col_hint', 'zh-CN', '处理建议', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.col_hint', 'en-US', 'Recommended action', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.hint_empty', 'zh-CN', '空 schema，确认后可交由 DBA 手工 DROP', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.hint_empty', 'en-US', 'Empty schema; once confirmed, a DBA can drop it manually', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.hint_filled', 'zh-CN', '里面有表，先确认数据是否还需要，再决定是否 DROP', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.hint_filled', 'en-US', 'Contains tables; confirm whether the data is still needed before dropping', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.missing_title', 'zh-CN', '产物对账巡检：注册行缺少 schema', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.missing_title', 'en-US', 'Artifact reconciliation: registry rows missing their schema', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.missing_desc', 'zh-CN', '这些插件声明了数据结构版本，但数据库里的 schema 不存在 —— 构建装配会在建表时失败。重装该插件即可重建（安装会先清旧 schema 再跑迁移）。', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.missing_desc', 'en-US', 'These plugins declare a schema version, but the schema does not exist in the database — assembly will fail while creating tables. Reinstalling the plugin rebuilds it (install drops the old schema first, then runs migrations).', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.orphan_storage_title', 'zh-CN', '产物对账巡检：孤儿存储目录', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.orphan_storage_title', 'en-US', 'Artifact reconciliation: orphan storage directories', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.orphan_storage_desc', 'zh-CN', '这些目录还留在磁盘上，但注册表里已经没有对应插件（卸载时删除目录失败）。巡检不自动删：目录里可能有还没迁走的资产，请先确认内容再手工删除。', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.orphan_storage_desc', 'en-US', 'These directories are still on disk, but no matching plugin remains in the registry (directory removal failed during uninstall). The patrol never deletes automatically: the directory may hold assets that were not migrated away, so check the contents before removing it by hand.', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.missing_storage_title', 'zh-CN', '产物对账巡检：注册行缺少存储目录', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.missing_storage_title', 'en-US', 'Artifact reconciliation: registry rows missing their storage directory', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.missing_storage_desc', 'zh-CN', '这些插件在注册表里，但磁盘上的目录不见了 —— 构建装配会静默跳过它们（组件库里少一组组件而没有任何提示）。重装该插件即可恢复。', 200, 'admin', 'admin/plugins.html', 1, now(), now()),
    ('admin.plugins.patrol.missing_storage_desc', 'en-US', 'These plugins are in the registry, but their directories are gone from disk — assembly silently skips them (a whole component group disappears from the palette with no warning). Reinstalling the plugin restores it.', 200, 'admin', 'admin/plugins.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
