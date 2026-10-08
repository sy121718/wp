#!/usr/bin/env python3
# button-code-scan.py — 模板按钮码扫描（check-button-code-binding.sh 的扫描器）。
#
# 单独成文件而不是内联在 shell 里：Jet 注释的定界符 `{*` / `*}` 与 shell 的引号规则
# 互相干扰，内联 heredoc 写出来极难读、也极易被改写弄坏。判据本身很短，放在这里一眼能看全。
#
# 两种模式（由 argv[1] 选择）：
#   text  —— 打印「退役写法 / 权限码形态」的命中行（文件:行号:内容），供方向 B 报错；
#   codes —— 打印模板引用的按钮码集合（每行一个，已排序去重），供方向 A 与数据库比对。
#
# 两个模式都**先剔注释**（Jet 的 {* *} 与 HTML 的 <!-- -->）：注释里举例说明写法是正常的，
# 不剔会把举例当成真实引用。
import glob
import re
import sys

JET_COMMENT = re.compile(r"\{\*.*?\*\}", re.S)
HTML_COMMENT = re.compile(r"<!--.*?-->", re.S)
# 退役写法：PermSet[...]（旧词汇）；权限码形态的按钮码：Buttons["a:b"]。
BAD = re.compile(r'PermSet\[|Buttons\["[a-z][a-zA-Z0-9_]*:[a-zA-Z0-9_]+"\]')
BUTTONS = re.compile(r'Buttons\["([^"]+)"\]')


def strip_comments(src: str) -> str:
    """把注释整段换成等长空白（保留行号与列位置）。"""
    blank = lambda m: re.sub(r"[^\n]", " ", m.group(0))
    return HTML_COMMENT.sub(blank, JET_COMMENT.sub(blank, src))


def main() -> int:
    mode = sys.argv[1] if len(sys.argv) > 1 else "text"
    files = sorted(glob.glob("internal/templates/**/*.html", recursive=True))
    if mode == "codes":
        codes = set()
        for path in files:
            codes.update(BUTTONS.findall(strip_comments(open(path, encoding="utf-8").read())))
        sys.stdout.write("\n".join(sorted(codes)) + "\n")
        return 0
    for path in files:
        src = strip_comments(open(path, encoding="utf-8").read())
        for lineno, line in enumerate(src.split("\n"), 1):
            if BAD.search(line):
                print(f"{path}:{lineno}:{line.strip()}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
