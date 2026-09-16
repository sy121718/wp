# l0-style-only — 纯样式声明档

零模板 CSS、零 `assets/`、零迁移、零预设：这个组件的每一个像素都由
`manifest.json` 的 `styles.rules` 声明出来。适用于「组件就是一堆既有元素的
重新组合 + 换皮」，不写任何手写 CSS 的插件作者。

## 安装（后台）

```bash
cd examples/l0-style-only
zip -r /tmp/styletile-1.0.0.zip manifest.json components
# 后台：插件管理 → 上传 /tmp/styletile-1.0.0.zip → 启用
```

安装后工作台组件库出现「提示条（纯样式声明）」（类型 `plugin.styletile.styletile_note`），
检查器有 6 个控件（标题 / 正文 / 语义 / 强调色 / 纵向内边距 / 悬浮位移）。

## 这个档位展示了什么

- `decls`：静态声明（盒模型、圆角、过渡）；
- `bindings`：`padY` 控件 → `padding`，`lift` 控件 → `transform: translateY(...)`；
- `when`：`tone` 三个枚举值各出一套配色（对标内置组件的 variant 系统）；
- `target` + `pseudo: first-child`：子元素样式与结构伪类；
- `vars`：`accent` 控件 → CSS 变量 `--st-accent`，同一条规则里的 `var(--st-accent)` 直接消费它；
- `pseudo: hover / hover-none / active`：悬浮（自动包 `@media (hover: hover)`）、
  触屏等价形态（`@media (hover: none)`）、按压反馈（不包媒体查询）；
- `breakpoints`：手机端减小内边距；
- `queries`：容器宽度 ≥ 480px 时横向排列（`@layer sky-auto`）、
  主题密度为 compact 时收紧内边距（`@layer sky-theme`）。

## 排错

- 上传被拒 `样式声明非法: 规则 N ...`：属性名不在白名单、值含非法字符、
  选择器不是 1~3 段类选择器 —— 报错行号即 `styles.rules` 的下标（从 0 起）；
- 上传被拒 `pseudo "hover" 走专用样式桶，不能同时声明 breakpoints`：
  悬浮/按压规则没有断点维度，把断点声明挪到另一条规则上；
- 装上了但页面没变化：确认插件是「启用」状态，且页面重新构建过
  （组件编译进产物，已发布页面需要重建）。

能力边界（能做什么 / 不能做什么）见 `docs/06-E-plugin-authoring.md`。
