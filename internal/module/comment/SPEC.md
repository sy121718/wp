---
id: go-wp-mod-comment
type: module-design
title: comment — 评论与审核
status: active
parent: go-wp-modules
---

# comment — 评论与审核

## 职责

评论表与「先审后发」状态机、限流、长度与字符校验、后台审核列表。

## 边界

- **不认识具体实体表**：某类实体允不允许评论，问注入的 `EntityPolicy`；策略缺失时按关闭降级，不猜默认。
- 模块内自足：没有跨模块契约依赖。
- 访客侧提交与展示经 runtime fragment 的片段端口进出。
