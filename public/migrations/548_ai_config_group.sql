-- 548 · AI 模块的全局开关组（sys_config 的 'ai' 组）
--
-- 为什么这个开关在 sys_config 而不在 config.yaml：它是**安全开关** —— 对外 MCP 接入点
--   （POST /mcp）是本站数据对外的出口，发现异常流量时的第一反应是关掉它。
--   改 config.yaml 要重启进程，关窗口的代价不该是一次重启。
--
-- 默认值 false（**默认关闭**）：口径见 docs/17 §P8「`/mcp` 默认关闭，显式开启才生效」。
--   端点在读不到本组、读不到这个键、或值不是 true 时一律按关闭处理（fail closed），
--   关闭时回 404 而不是 403 —— 探测者不该从响应里知道「这里有个可以打开的东西」。
--
-- 幂等：ON CONFLICT (group_key) DO NOTHING。改默认值请新增迁移用 UPDATE；
--   直接改本文件对已执行过的库无效（ConditionSQL 会判为完成并跳过，见 AGENTS.md「迁移」）。

INSERT INTO sys_config (group_key, group_name, config_data, remark, status, version, create_by, create_time, update_by, update_time)
VALUES ('ai', 'AI 与外部接入', '{"mcp_enabled": false}'::jsonb,
        'mcp_enabled：对外 MCP 接入点（POST /mcp）是否可达。默认关闭，必须显式打开；关闭时端点对任何请求都回 404。', 1, 1, 0, now(), 0, now())
ON CONFLICT (group_key) DO NOTHING;
