---
id: go-wp-mod-seo
type: module-design
title: seo — 跨层 SEO 横切
status: active
parent: go-wp-arch
---

# seo — 跨层 SEO 横切

## 职责

URL 规范化与绝对化、sitemap / robots / feed 生成、评分（商品 / 落地页 / 指南三类画像）、合规检查、搜索索引提交，以及页面文档与文章到评分输入的抽取。

## 边界

- **零业务依赖、无表、无路由**：刻意放在能同时被编译内核、发布内核与业务模块引用的位置；它不 import builder、pipeline 或任何模块。
- **评分规则只有一份**：页面与文章共用同一套评分，不在别处复制。
- 语言规则的定义真源在 `internal/pipeline`，本模块只用不定义（反向 import 会成环）。
- 标题/描述等文案的唯一注入点在构建器的 SEO head。
