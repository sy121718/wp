# HTMX 扩展评估（go_wp 适用性）

> 目的：记录 htmx 官方扩展对本项目的适用性，避免「能用现成能力却手写 JS」。
> 状态：开发期评估，逐个按需引入、本地托管、单独提交。
> 相关：`AGENTS.md` §交互方式（HTMX）、`docs/06-plugin-system.md`、`docs/09-session-handoff.md`

## 一、体积基线

| 资源 | 体积 | 备注 |
|---|---|---|
| `htmx.min.js`（2.x） | ~48KB 未压缩 / **~14KB gzip** | 后台页面引入；访客面为零 JS 产物，不受影响 |
| `idiomorph.min.js` | 10.6KB 未压缩 / **~3.8KB gzip** | morphing swap 策略（已引入，见 §四） |
| 单个扩展典型 | 2~5KB | 逐个评估引入 |

**结论**：htmx 本体不重（gzip 后与一张小图相当），扩展单价更低；真正的成本是「多一个要维护的依赖」，所以每个扩展都要写清收益。

## 二、扩展清单与适用性

| 扩展 | 作用 | 对本项目的价值 | 结论 |
|---|---|---|---|
| **idiomorph** | DOM morphing（只改差异节点，保留未变节点状态） | 检查器 / 页面设置 / 全局设置 / 历史面板目前是 `panel.innerHTML = html` 整块重建——滚动位置、输入焦点、展开的分组全部复位；morph 后原地保留 | ⭐ **优先引入** |
| **head-support** | swap 时合并 `<head>`（style/meta/title） | 可替代画布桥接里手写的 `<style id="wb-live-css">` 替换逻辑 | 第二步评估 |
| **response-targets** | 按响应码（4xx/5xx）定向到不同 target | 错误提示改为声明式，省掉 fetch `.catch()` 里的手写分支 | 可引入 |
| **loading-states** | 纯属性驱动 loading 态（`data-loading-disable` 等） | 存草稿 / 发布 / 构建按钮的「提交中」不用写 JS | 可引入 |
| **class-tools** | 按时间切换 class | toast 自动消失、入场动画 | 低优先 |
| **preload** | 悬停/进入视口预加载 | 画布与后台页面跳转更快 | 低优先 |
| **remove-me** | 定时移除元素 | 提示条自动消失（与 class-tools 重叠） | 低优先 |
| **multi-swap** | 一次替换多个区域 | 本项目片段多为单区域更新 | 不需要 |
| **path-deps** | 路径依赖失效刷新 | 本项目路由用 Query 参数、非 REST 风格 | 不需要 |
| **ws** | WebSocket 支持 | 未来「多人在线协作编辑」才有意义 | 暂不需要 |
| **sse** | Server-Sent Events | 未来实时通知/构建进度推送 | 暂不需要 |
| **json-enc** | 请求体编码为 JSON | 现有 API 表单/JSON 混用，需逐个核对是否受益 | 待评估 |
| **client-side-templates** | 客户端模板（mustache/handlebars） | 与「服务端 Jet 渲染」原则直接冲突 | **不需要** |
| **event-header** | 触发事件信息放进请求头 | 当前无场景 | 不需要 |
| **restored** | 恢复页面/滚动状态 | 当前无场景 | 不需要 |
| **debug** | 调试日志 | 开发期可选，release 必须关闭 | 可选 |
| **alpine-morph** | Alpine + morph | 项目不用 Alpine | 不需要 |

> 版本与 API 以官方文档为准（htmx 2.x 的扩展加载方式与 1.x 不同：扩展是独立脚本，需在 htmx 之后引入并调用 `htmx.defineExtension` 或直接使用其全局对象）。引入前用 context7 核对当前版本 API。

## 三、引入规范

1. **本地托管**：`internal/templates/static/vendor/htmx/`（与 `vendor/trix/` 同一规范），不跟 CDN 漂移
2. **版本锁定**：文件名或目录带版本，升级单独提交
3. **逐个评估**：每个扩展附「收益 / 代价 / 替代方案」三行说明，收益不明确就不引入
4. **优先级**：htmx 原生能力 → 官方扩展 → 自定义 JS（自定义 JS 是最后手段）
5. **访客面**：以上全部只作用于后台控制面；公开站点产物仍是零 JS（Runtime Fragment 除外）

## 四、首个落地项：idiomorph

**问题**：`fetchInspectorPanel` / 设置面板 / 历史面板均为 `panel.innerHTML = html`，整块重建导致展开状态、滚动位置、输入焦点复位。

**改法**：把 `innerHTML` 替换为 morph：
```js
Idiomorph.morph(panel, html, { morphStyle: 'innerHTML' });
```

**边界**：idiomorph **不能跨 iframe**——画布的 `patchCanvas`（postMessage 节点替换）仍保留；除非未来把画布改为同页 DOM。

**验收**：改属性后面板滚动位置与输入焦点保持；展开的分组不收起；无新增 JS 错误。

## 五、Trix 附件上传（现状记录）

富文本编辑器（Trix 2.x，本地 vendor）的附件上传已接 **`POST /api/media/upload`**——复用 media 模块的 multipart 上传、类型/大小/魔数校验与存储，字段名 `file`；**非超管需 `media:upload` 权限点**（seed 见迁移 `030_business_permissions.sql` / `031_business_permissions_superadmin.sql`）。前端在 `trix-attachment-add` 事件里 XHR 上传后回填 `attachment.setAttributes({ url, href })`，失败则移除 pending 附件（避免空 `<figure>` 写进正文）；不新增代理端点。
