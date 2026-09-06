-- 040_plugin_registry.sql — 插件注册表（docs/06-plugin-system.md §8）。
-- 插件 = 编译期输入的数据包（模板/样式声明/迁移/资产），registry 记账：
--   plugin_id     manifest.id（如 "marketing"，小写目录安全字符）
--   version       语义化版本（升级比对，增量迁移依据）
--   schema_version L1 数据层迁移版本（0 = 无自有表）
--   enabled       启停（停用即不参与构建：CompositeLoader 与 palette 忽略）
--   manifest      完整 manifest 快照（安装时校验后的可信版本）
--   storage_path  解包目录（public/runtime/plugins/{id}/{version}/，不对外暴露）
-- 卸载 = 删行 + 级联删存储目录（L1 场景另需 DROP SCHEMA，由 service 编排）。

CREATE TABLE IF NOT EXISTS plugin_registry (
    plugin_id     text PRIMARY KEY,
    name          text NOT NULL,
    version       text NOT NULL,
    schema_version int NOT NULL DEFAULT 0,
    enabled       boolean NOT NULL DEFAULT false,
    manifest      jsonb NOT NULL,
    storage_path  text NOT NULL DEFAULT '',
    installed_at  timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE plugin_registry IS '插件注册表：安装/版本/启停记账（docs/06-plugin-system.md）';
