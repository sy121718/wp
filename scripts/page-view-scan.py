#!/usr/bin/env python3
"""page-view-scan.py <file> → 打印 "<gin.H 页面视图数> <c.HTML 数>"

check-page-view-dto.sh 的扫描器。单独成文件而不是内嵌在 shell 里：
判据本身（什么算页面视图）值得被读、被单独测，而不是藏在一段 heredoc 里。

## 判据

**一条 gin.H 算页面视图，当且仅当它所在的那条语句最终交给模板。**

所以按**语句**判定，不按行：从一行开始累加括号深度，回到零就是语句结束。
整条语句里出现 JSON 出口函数（response.Success / c.JSON / writeRPCResult / …），
其中的 gin.H 一律不计入。

从前是单行判定，并注明「跨行时宁可多算」。那个保守取法的代价后来变成了实质问题：
台账挂着一批跨行写的 JSON-RPC 载荷，永远迁不掉 —— 数字看着像永远做不完。

不计入的四类：
  · JSON 出口语句里的 gin.H（不是模板契约）
  · 空字面量 gin.H{}（没有键，不可能是视图）
  · []gin.H / map[string]gin.H（容器类型与 append）
  · **内部结构构造器**：函数签名返回 map[string]any / []gin.H / map[string]gin.H 时，
    函数体里攒的 gin.H 是中间产物，不是一个视图。典型形态是把页面脚本要用的
    元数据摊成 map 再 json.Marshal 成字符串槽（模板读的是那个字符串，不是这个 map）。
    不排除它，门禁会把这类合法写法永久挂成"欠账"。

注意 `type Xxx gin.H` 这一行本身不算，但它的字面量仍按语句计入 —— 具名视图别名是
「显式命名的视图」，本门禁要消灭的是「控制器里现场拼一份 map」，不是它。
"""
import re
import sys

JSON_EGRESS = re.compile(
    r"response\.Success|response\.Error|response\.OK|SuccessWithMessage"
    r"|\.JSON\(|writeRPCResult|writeJSON|json\.Marshal"
    r"|^type [A-Za-z]+ gin\.H"
)
# 容器与类型声明：[]gin.H / map[string]gin.H 的出现位置不只是字面量，
# 还有**函数签名**（`func (e *X) toolsFor(...) []gin.H {`）—— 那一行的 `gin.H` 后面
# 紧跟 `{`，看起来像一个 gin.H 字面量。判据是「前面紧邻 [] 或 map[string]」。
CONTAINER = re.compile(r"(\[\]|map\[string\])gin\.H|gin\.H\{\}")


def statements(src):
    """切语句。

    两条边界：
      · 括号深度归零（一条完整的表达式 / 调用结束）；
      · 顶格开始的 `func` / `type` / `var` / `const`（Go 的顶层声明）。

    第二条是必须的：函数体内「深度先归零、紧跟一串无括号的语句」（比如
    `out := make([]gin.H, 0)` 之后接一个 for 循环）如果只按深度切，
    会把整个函数黏成一条语句 —— 于是函数体里任意一处 gin.H 都会被
    整条语句上的 JSON 出口顺带豁免（或顺带计入）。这两种误判都出现过。
    """
    out, cur, depth = [], [], 0
    for line in src.split("\n"):
        code = re.sub(r"//.*$", "", line)
        starts_toplevel = bool(re.match(r"^(func|type|var|const)\b", line))
        if starts_toplevel and depth == 0 and cur:
            out.append("\n".join(cur))
            cur = []
        cur.append(line)
        depth += code.count("(") + code.count("{") + code.count("[")
        depth -= code.count(")") + code.count("}") + code.count("]")
        if depth <= 0:
            out.append("\n".join(cur))
            cur, depth = [], 0
    if cur:
        out.append("\n".join(cur))
    return out


# NON_VIEW_RETURN 函数签名返回「内部结构」而非视图：函数体里的 gin.H 不算页面视图。
#
# 判据是返回类型，不是函数名（名字随时会改）。三种返回形态都是中间产物：
#   map[string]any / map[string]interface{}  —— 元数据表，通常随后 json.Marshal
#   []gin.H / map[string]gin.H               —— 控件/选项列表，之后还会被再加工
NON_VIEW_RETURN = re.compile(
    r"^func\b[^{]*\b(map\[string\](any|interface\{\})|\[\]gin\.H|map\[string\]gin\.H)\s*\{"
)


def main():
    path = sys.argv[1]
    src = open(path, encoding="utf-8").read()
    gin = 0
    for st in statements(src):
        if JSON_EGRESS.search(st):
            continue
        # 内部结构构造器：它的 gin.H 不进模板。
        if NON_VIEW_RETURN.search(st.strip()):
            continue
        # 注释里提到的 gin.H（技术说明）不是代码：先把注释行剔掉再数。
        code_only = "\n".join(re.sub(r"//.*$", "", ln) for ln in st.split("\n"))
        for m in re.finditer(r"gin\.H\{", code_only):
            # 空字面量与容器声明不算（它们是「没有键」与「类型」，不是一份视图）。
            # 前缀要在**整条语句**里往前看，不能只看当前位置（`[]gin.H {` 的 `{` 是函数体）。
            prefix = code_only[max(0, m.start() - 24):m.start()]
            if CONTAINER.search(prefix) or re.search(r"append\([^)]*,\s*$", prefix):
                continue
            # 赋值形式的空字面量（`data := gin.H{}` / `data = gin.H{}`）：
            # 它是「后面再往里塞键的临时容器」，不是一份拼好的视图。
            if re.match(r"gin\.H\{\s*\}", code_only[m.start():]):
                continue
            gin += 1
    print(gin, src.count("c.HTML("))


if __name__ == "__main__":
    main()
