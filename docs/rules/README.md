# docs/rules —— 规则细则（按需加载）

`AGENTS.md` 是**常驻规则**：只放判据、红线与指向本目录的指针。
本目录是**规则细则**：判据背后的论证、实测数据、踩坑实例与完整操作步骤。

判据本身在 `AGENTS.md` 里一句话就能读完；需要「为什么这么定」「上次怎么踩的」「具体怎么验」时再来这里。

## 什么时候读哪份

| 场景 | 读 |
|---|---|
| 建表 / 改列 / 写迁移 / 选主键 / 碰 RLS / 配 datarule | [`database.md`](database.md) |
| 写测试 / 改测试基建 / 下结论前的验证 | [`testing.md`](testing.md) |
| 改组件 / 改样式 / 交互改动 / 多端适配 | [`frontend.md`](frontend.md) |
| 写词条 / 改文案 / 碰 `?err=` 一类 URL 回执通道 | [`i18n.md`](i18n.md) |
| 并行批次执行者（子代理） | [`../agents/parallel-batch-rules.md`](../agents/parallel-batch-rules.md) |

## 分工

```
AGENTS.md               常驻：判据 + 红线 + 指针（每轮都进上下文，必须短）
docs/rules/*.md         细则：论证 + 实测 + 步骤（按需读）
docs/agents/*.md        面向子代理的工作规则
docs/*.md               项目规划 / 规格 / 审计 / 模块清单（项目内容，不是规则）
```

新增规则时的归属判据：**能写成一句话判据的进 `AGENTS.md`；需要超过三行才说得清为什么的进本目录。**
