#!/usr/bin/env python3
"""page-view-scan 的判据测试。

为什么扫描器需要自己的测试：它是 check-page-view-dto 的**判据本体**，
误判的代价是双向的 ——
  · 多算：合法写法被永久挂成"欠账"，台账永远收不到零（mail 的
    automationCanvasMeta 就是这样被挂住的）；
  · 少算：真视图混过去，门禁变成摆设。
所以每条排除/计入规则都要有一条用例钉住，而不是靠读代码相信它。

用法：python3 scripts/tests/test_page_view_scan.py
"""
import importlib.util
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
SCAN = os.path.join(HERE, "..", "page-view-scan.py")


def load_scanner():
    spec = importlib.util.spec_from_file_location("page_view_scan", SCAN)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def count(src):
    """在源码字符串上跑判据，返回 (gin.H 页面视图数, c.HTML 数)。

    **直接调用扫描器暴露的 count()**，不在这里复刻主循环 —— 复刻一份的后果是：
    改扫描器判据（新豁免、窗口长度）时这里仍全绿，而真门禁已经漂移，
    正是本仓「同一套判据写两处」的老问题。
    """
    return load_scanner().count(src)


CASES = [
    (
        "交给模板的 gin.H 计为页面视图",
        """package x
func handler() {
    pageview.Render(c, gin.H{"Title": "t"})
}
""",
        1,
    ),
    (
        "JSON 出口语句里的 gin.H 不计",
        """package x
func handler() {
    response.Success(c, gin.H{"ok": true})
}
""",
        0,
    ),
    (
        "空字面量与容器不计",
        """package x
func handler() {
    data := gin.H{}
    rows := make([]gin.H, 0)
    _ = data
    _ = rows
}
""",
        0,
    ),
    (
        "返回 map[string]any 的内部结构构造器不计",
        """package x
func meta() map[string]any {
    entry := gin.H{"value": 1}
    return map[string]any{"nodeTypes": entry}
}
""",
        0,
    ),
    (
        "返回 []gin.H 的选项构造器不计",
        """package x
func options() []gin.H {
    out := append(make([]gin.H, 0), gin.H{"Value": 1})
    return out
}
""",
        0,
    ),
    (
        "容器构造器内部的 gin.H 也不计（append 形态）",
        """package x
func palette(raw any) []gin.H {
    list, _ := raw.([]gin.H)
    out := make([]gin.H, 0, len(list))
    for _, t := range list {
        out = append(out, gin.H{"Value": t})
    }
    return out
}
""",
        0,
    ),
    (
        "返回 map[string]any 但内部有渲染出口时仍计入",
        """package x
func pageMap() map[string]any {
    pageview.Render(c, gin.H{"Title": "t"})
    return nil
}
""",
        1,
    ),
    (
        "同一文件里，视图与内部构造器分别判定",
        """package x
func meta() map[string]any {
    return map[string]any{"a": gin.H{"k": 1}}
}
func handler() {
    pageview.Render(c, gin.H{"Title": "t"})
}
""",
        1,
    ),
]


def main():
    failed = 0
    for name, src, want_gin in CASES:
        got_gin, _ = count(src)
        if got_gin != want_gin:
            print(f"✗ {name}：gin.H 页面视图 {got_gin}，应为 {want_gin}")
            failed += 1
        else:
            print(f"✓ {name}")
    if failed:
        print(f"\n{failed} 条判据不符", file=sys.stderr)
        return 1
    print(f"\n全部 {len(CASES)} 条判据通过")
    return 0


if __name__ == "__main__":
    sys.exit(main())
