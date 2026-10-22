---
id: go-wp-mod-webhook
type: module-design
title: webhook — 出站事件投递
status: active
parent: go-wp-modules
---

# webhook — 出站事件投递

## 职责

投递端点、投递记录、HMAC 签名（时间戳 + 原始报文）、SSRF 防护（解析后逐 IP 判定）、异步投递与失败重放。

## 边界

- 事件发布方只调「派发」，真正的出站 HTTP 由后台 worker 执行，不拖住业务请求。
- 零跨模块依赖：本模块不认识产生事件的业务模块。
