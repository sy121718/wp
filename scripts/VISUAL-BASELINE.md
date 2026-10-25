# 视觉回归基线

## 为什么有它

第三批要把散落的色值（199 处）与 px 尺寸（652 处）换成令牌。这类改动**不会让任何
Go 测试变红**：产物字节变了，而 golden 只比对「和上一次生成的一样」，重新生成 golden
是例行操作。于是「颜色改了」这件事没有任何东西能发现。

唯一的判据是**看起来是否一样**。截图基线就是那个判据。

## 用法

```bash
# 1. 动手改之前：建立基线
python3 scripts/visual-baseline.py --save

# 2. 改完之后：比对
python3 scripts/visual-baseline.py --check

# 3. 只渲某几个组件
python3 scripts/visual-baseline.py --save --only card,heading
```

- 基线：`testdata/visual-baseline/<组件>.png`（22 个 golden 组件，352K）
- 差异图：`.visual-diff/<组件>-region.png`（只截变化区域，人工复核用）

## 判据

**允许 0 像素差异。** 令牌替换是把 `border-radius: 6px` 写成 `border-radius: var(--radius-md)`，
取值必须完全一致。有差异就说明换了值 —— 那是「改视觉」而不是「换写法」，必须单独评审，
不能混在规范化里。

## 两个必须处理的确定性陷阱

都实测踩过，两者都会让基线变成「每次都不一样」而失去意义：

1. **外部网络**。golden 里有 `https://example.com/a.jpg`、YouTube iframe。让它们真的
   去请求，截图就依赖网络延迟与远端内容。做法：拦掉全部 `**://**` 请求。

2. **iframe 元素本身**。只拦请求**不够** —— `<iframe src="https://youtube…">` 即使请求被
   abort，元素仍会插入文档参与布局，而它的最终尺寸取决于 abort 时机（video 组件实测在
   0~0.077% 之间随机抖动）。做法：把 `iframe/video/audio/object/embed` 整体摘掉。

   代价是截图不含 iframe 内部内容。这是刻意的 —— 基线要回答「我们的 CSS 改了吗」，
   不是「YouTube 返回了什么」，后者本来也不该进基线。

处理完这两点后连跑三次 `--check` 都是零差异（实测）。

## 前置

golden 由 `go test ./internal/builder/ -run TestJetViewByteEquivalent` 维护（`-update-jet-golden`
可重生成）。本脚本把 `<name>.html` + `<name>.css` 拼成独立页面再截图，与 Go 侧 golden
是同一份真相的两个视角。
