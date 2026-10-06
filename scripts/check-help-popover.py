#!/usr/bin/env python3
"""卡片口径说明必须走 .help-pop 浮层，不许用原生 title。

判据为什么是这个：

原生 `title` 的问题不是「不好看」而是**位置不可控** —— 悬浮层由浏览器绘制，不受 z-index
约束、看不见键盘焦点、触屏不出现。实测它悬浮时盖住相邻卡片，看上去就是「说明文字直接
显示在卡面上」，被当成「把注释写进了前端」（2026-10 第二次犯，见 internal/templates/CLAUDE.md
§「说明文字一律走 .help-pop」）。

**唯一例外**：纯图标按钮的无障碍名字（关闭 / 切换主题 / 语言 / 站点、柱图的按月 tooltip）。
它们没有可见文字，改成浮层反而要点一次才读得到 —— 本脚本只查**卡片类元素**，这类按钮
不落在卡片上，所以不需要白名单。

用法：python3 scripts/check-help-popover.py [--quiet]
退出码：0 = 通过；1 = 有卡片元素仍在使用 title。
"""

from __future__ import annotations

import os
import re
import sys
from html.parser import HTMLParser

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SCAN_DIRS = [os.path.join(ROOT, "internal", "templates")]

# 卡片类元素的判据：类名 token 等于 card、或以 stat-card 开头。
# 刻意**不含** card-title / card-header / card-body —— 那是卡片内部的小节，
# 在它们上面挂 title 属于另一类问题（不在这里守）。
def is_card_class(class_attr: str) -> bool:
    for token in class_attr.split():
        if token == "card" or token.startswith("stat-card"):
            return True
    return False


class CardTitleFinder(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.hits: list[tuple[int, str, str]] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        attrib = {k: (v or "") for k, v in attrs}
        if not is_card_class(attrib.get("class", "")):
            return
        title = attrib.get("title")
        if title is None:
            return
        self.hits.append((self.getpos()[0], tag, title.strip()))


# Jet 模板里的 {* … *} 注释会混进标签流，先按行剥掉再解析。
COMMENT_RE = re.compile(r"\{\*.*?\*\}", re.S)


def scan_file(path: str) -> list[tuple[int, str, str]]:
    with open(path, "r", encoding="utf-8") as fh:
        src = fh.read()
    src = COMMENT_RE.sub(lambda m: " " * len(m.group(0)), src)
    parser = CardTitleFinder()
    parser.feed(src)
    parser.close()
    return parser.hits


def main() -> int:
    quiet = "--quiet" in sys.argv
    failed = False
    checked = 0

    for base in SCAN_DIRS:
        for dirpath, _dirnames, filenames in os.walk(base):
            for name in sorted(filenames):
                if not name.endswith(".html"):
                    continue
                path = os.path.join(dirpath, name)
                rel = os.path.relpath(path, ROOT)
                checked += 1
                hits = scan_file(path)
                if not hits:
                    continue
                failed = True
                for lineno, tag, title in hits:
                    shown = title if len(title) <= 60 else title[:57] + "…"
                    print(f"{rel}:{lineno}  <{tag} class=\"…card…\"> 仍用原生 title：{shown}")

    if failed:
        print(
            "\n✗ 卡片口径说明必须走 .help-pop 浮层（标签旁的 ? 按钮），不要用原生 title。\n"
            "  形态见 internal/templates/CLAUDE.md §「说明文字一律走 .help-pop，不用原生 title」。\n"
            "  卡片改浮层时记得同时放开 overflow（.card.sales-card / .card.stat-card）。"
        )
        return 1

    if not quiet:
        print(f"✓ 卡片口径说明都走浮层（已扫 {checked} 个模板）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
