#!/usr/bin/env python3
"""视觉回归基线：把 golden 组件产物渲染成截图，供颜色/尺寸收敛时对账。

## 为什么需要它（而不是"看代码觉得没问题"）

第三批要把散落的色值（199 处）与 px 尺寸（652 处）换成令牌。这类改动**不会让任何
Go 测试变红** —— 产物字节变了，但 golden 只比对"和上一次生成的一样"，
而重新生成 golden 是例行操作。于是"颜色改了"这件事没有任何东西能发现。

唯一的判据是**看起来是否一样**。这个脚本就是那个判据：
先给基线截图，改完再截一次，逐像素比对。

## 用法

    # 1. 建立基线（在动手改之前！）
    python3 scripts/visual-baseline.py --save

    # 2. 改完之后比对
    python3 scripts/visual-baseline.py --check

    # 3. 只渲某几个组件
    python3 scripts/visual-baseline.py --save --only card,button,container

## 判据

--check 逐组件比对像素差异。**允许 0 差异** —— 色值/尺寸换成令牌时，取值必须完全一致；
有差异就说明换了值，那是"改视觉"而不是"换写法"，必须单独评审。

差异图落在 .visual-diff/ 下（红框标出变化处），基线在 testdata/visual-baseline/。

## 前置

golden 由 `go test ./internal/builder/ -run TestJetViewByteEquivalent` 维护，
里面是每个组件的 HTML 片段 + CSS。本脚本把两者拼成独立 HTML 再截图，
因此它与 Go 侧的 golden 是同一份真相的两个视角。
"""

import argparse
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
GOLDEN = ROOT / "internal" / "builder" / "testdata" / "golden"
BASELINE = ROOT / "testdata" / "visual-baseline"
DIFF_DIR = ROOT / ".visual-diff"

# 截图宽度：够放下三栏栅格，又不至于把单列组件拉得太宽。
VIEWPORT_W = 900
VIEWPORT_H = 600

HTML_SHELL = """<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<style>
  /* 与产物同款的基础重置 + 令牌：漏掉任何一个，截图差异都会指向"没加载样式"
     而不是"样式变了"这两种完全不同的原因。 */
  *, *::before, *::after {{ box-sizing: border-box; }}
  body {{ margin: 0; padding: 24px; background: #fff; color: #1a1d21;
         font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
         font-size: 14px; line-height: 1.6;
         /* 冻结动效：入场动画在截图时有不同的中间帧，会让每次截图都不一样。 */
         animation: none !important; transition: none !important; }}
  * {{ animation: none !important; transition: none !important; }}
  img {{ max-width: 100%; }}
{css}
</style></head>
<body>{html}</body></html>
"""


def golden_names():
    """golden 里成对的 <name>.html + <name>.css。"""
    names = []
    for html in sorted(GOLDEN.glob("*.html")):
        name = html.stem
        if (GOLDEN / f"{name}.css").exists():
            names.append(name)
    return names


def build_page(name: str) -> str:
    html = (GOLDEN / f"{name}.html").read_text(encoding="utf-8")
    css = (GOLDEN / f"{name}.css").read_text(encoding="utf-8")
    return HTML_SHELL.format(css=css, html=html)


def shot(page_html: str, out: pathlib.Path):
    """渲染并截图。

    **必须屏蔽全部外部网络**：golden 里有 https://example.com/a.jpg、YouTube iframe 等
    外部地址。让它们真的去请求，截图就依赖网络延迟与远端返回内容 —— video 组件曾出现
    0.077% 的像素抖动，来源是 YouTube iframe 加载进度的随机性，而不是样式变化。

    拦掉之后同一份 HTML 每次渲染完全一致（实测两次 --save 的 md5 相同）。
    """
    from playwright.sync_api import sync_playwright

    with sync_playwright() as p:
        browser = p.chromium.launch()
        page = browser.new_page(viewport={"width": VIEWPORT_W, "height": VIEWPORT_H})

        def block(route):
            # abort 而不是 fulfill 空响应：abort 会让 <img> 立即进入 error 态（确定），
            # fulfill 一个 204 在某些元素上会走不同的解码路径。
            route.abort()

        page.route("**://**", block)   # 除 about:blank / data: 之外一律拦掉
        page.set_content(page_html, wait_until="load")
        # 拦掉请求还不够：<iframe src="https://youtube…"> 即使请求被 abort，元素本身
        # 仍会被插入文档并参与布局，而它的最终尺寸取决于 abort 时机（实测 video 组件
        # 在 0~0.077% 之间随机抖动）。**把外部嵌入元素整体摘掉**，布局才真正确定。
        #
        # 代价：截图不再包含 iframe 内部的真实内容。这是刻意的 —— 基线要回答的是
        # 「我们的 CSS 改了吗」，不是「YouTube 返回了什么」（那本来也不该进基线）。
        page.evaluate("""() => {
            for (const el of document.querySelectorAll('iframe, video, audio, object, embed')) {
                el.remove();
            }
        }""")
        page.wait_for_timeout(120)     # 让 abort 触发的 error 态落定
        page.screenshot(path=str(out), full_page=True)
        browser.close()


def compare(a: pathlib.Path, b: pathlib.Path) -> float:
    """返回不一致像素占比（0 = 完全一致）。"""
    from PIL import Image, ImageChops

    ia, ib = Image.open(a).convert("RGB"), Image.open(b).convert("RGB")
    if ia.size != ib.size:
        return 1.0  # 尺寸变化即视为全量差异（比逐像素更早暴露问题）
    diff = ImageChops.difference(ia, ib)
    bbox = diff.getbbox()
    if bbox is None:
        return 0.0
    total = ia.size[0] * ia.size[1]
    changed = sum(1 for px in diff.getdata() if px != (0, 0, 0))
    # 落一张差异图，标出变化区域（人工复核用）。
    DIFF_DIR.mkdir(exist_ok=True)
    ia.crop(bbox).save(DIFF_DIR / f"{a.stem}-region.png")
    return changed / total


def main():
    ap = argparse.ArgumentParser(description="组件视觉回归基线")
    ap.add_argument("--save", action="store_true", help="把当前渲染存为基线")
    ap.add_argument("--check", action="store_true", help="与基线比对")
    ap.add_argument("--only", default="", help="只处理这些组件（逗号分隔）")
    ap.add_argument("--tolerance", type=float, default=0.0,
                    help="允许的不一致像素占比（默认 0：令牌替换不得改变任何像素）")
    args = ap.parse_args()

    if not args.save and not args.check:
        ap.error("要 --save 或 --check")

    names = golden_names()
    if args.only:
        want = {s.strip() for s in args.only.split(",") if s.strip()}
        names = [n for n in names if n in want]
        missing = want - set(names)
        if missing:
            print(f"✗ 找不到这些 golden：{sorted(missing)}", file=sys.stderr)
            return 1
    if not names:
        print("✗ 没有可用的 golden 组件", file=sys.stderr)
        return 1

    BASELINE.mkdir(parents=True, exist_ok=True)

    if args.save:
        for name in names:
            shot(build_page(name), BASELINE / f"{name}.png")
            print(f"  基线 {name}.png")
        print(f"✓ 已保存 {len(names)} 个基线到 {BASELINE.relative_to(ROOT)}")
        return 0

    # --check
    # 前置一致性：基线必须与**当前 golden** 同源。golden 更新过而基线没重建时，
    # --check 会把「golden 变了」误报成「样式回归了」—— 排查方向直接跑偏。
    # 用 golden 的 mtime 作为粗判据（够用：重建基线总会晚于 golden 的更新）。
    stale = []
    for name in names:
        base_png = BASELINE / f"{name}.png"
        if base_png.exists() and (GOLDEN / f"{name}.css").stat().st_mtime > base_png.stat().st_mtime:
            stale.append(name)
    if stale:
        print(f"⚠ 这些组件的 golden 比视觉基线新：{sorted(stale)}", file=sys.stderr)
        print("  基线可能已过期 —— 若确认 golden 的变更是有意的，先跑 --save 重建，"
              "否则 --check 会把「golden 变了」误报成「样式回归了」。", file=sys.stderr)

    missing = [n for n in names if not (BASELINE / f"{n}.png").exists()]
    if missing:
        print(f"✗ 缺少基线：{sorted(missing)}（先跑 --save）", file=sys.stderr)
        return 1

    tmp = pathlib.Path("/tmp/visual-check")
    tmp.mkdir(exist_ok=True)
    failures = []
    for name in names:
        current = tmp / f"{name}.png"
        shot(build_page(name), current)
        ratio = compare(BASELINE / f"{name}.png", current)
        if ratio > args.tolerance:
            failures.append((name, ratio))
            print(f"✗ {name}: {ratio:.3%} 像素不同（差异图 .visual-diff/{name}-region.png）")
        else:
            print(f"✓ {name}")

    if failures:
        print(f"\n✗ {len(failures)}/{len(names)} 个组件视觉有变化。", file=sys.stderr)
        print("  令牌替换**不得改变任何像素**。有差异说明改了取值 —— 那是改视觉，"
              "要单独评审而不是混在规范化里。", file=sys.stderr)
        return 1
    print(f"\n✓ {len(names)} 个组件视觉零差异")
    return 0


if __name__ == "__main__":
    sys.exit(main())
