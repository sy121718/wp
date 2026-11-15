# 商品后台通用写反馈任务：已并入框架任务书

当前状态：仅整理任务文档，尚未实施代码。

本文件原先的实施方案已由 [框架基础归一与可视化工具化任务书](TASK-framework-foundation.md) 接替，不再作为独立任务执行；本文件只保留背景与验证入口索引，不定义第二套成功反馈契约。

## 已合并的目标

- 普通界面继续使用 Jet + HTMX，统一成功/失败反馈只消费消息和明确错误事实，不解释任意业务 `data`。
- Controller 不指定通用提示碎片；共享反馈、Jet 渲染、公共模板与资产由框架提供，不再安排 `internal/adminui` 承担具体渲染。
- 全应用一套公共 UI、一个 `/static` 资源入口；模板组合公共组件，每个表单/抽屉的反馈宿主独立。
- 商品创建已经由实体事实生成编辑链接，并由发起模板声明导航；迁移保持这条闭环，不重做导航协议、不把实体视图降成纯提示。
- 翻译拒绝保留逐行事实、真实状态码和整批回滚；早期拒绝、迟到响应、双宿主隔离、输入保留和安全边界仍须验证。

## 原始验证入口索引

执行者以新任务书和实际源码为准，不按历史行号或测试名猜测结果。

- [模块内 HTML 写链路](internal/module/product/inbound/http/product_html_write_test.go)
- [创建表单协议](internal/module/product/inbound/http/product_create_form_protocol_test.go)
- [真实数据库 HTML 写链路](public/test/product/feature/product_html_write_test.go)
- [翻译保存](public/test/product/feature/product_translation_test.go)
- [翻译失败与回滚](public/test/product/feature/product_translation_failure_test.go)
- [商品浏览器链路](public/test/product/feature/product_html_browser_test.go)
- [写表单协议门禁](public/test/architecture/write_form_protocol_test.go)

不再执行旧方案中的“模板引擎不得进 pkg”“成功渲染留 adminui”或默认运行 `check-all.sh` 等安排。规范仍唯一来自 [AGENTS.md](AGENTS.md)；本次只修订任务文档，不修改规范、代码、迁移或目录，也不运行构建、测试、门禁或提交。
