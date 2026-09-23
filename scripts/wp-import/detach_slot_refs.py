"""摘掉页面文档里「重复的页眉 / 页脚引用节点」（VIS-001 的存量收口）。

背景：页眉页脚的唯一入口是**站点结构**（settings.structure 的槽位绑定，编译期展开成
root 首尾的 core.layoutSlot 节点）。早期拼装流程把页眉页脚当成普通内容写进了页面文档
（core.globalref 引用同一个块），而编译期的槽位去重只认 core.layoutSlot 类型，认不出
core.globalref —— 于是页面上出现两份页眉，且两份字节完全相同（都是同一个块展开的）。

本脚本把「引用本站槽位块的 core.globalref 节点」从页面文档里删掉（整棵树，不只顶层：
放进了容器里的同样要摘），页眉页脚随后完全由站点结构提供。

用法：
    python3 scripts/wp-import/detach_slot_refs.py            # 只报告，不写
    python3 scripts/wp-import/detach_slot_refs.py --apply    # 保存 + 构建 + 发布
"""
from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

DEFAULT_LANG = os.environ.get("WP_DEFAULT_LANG", "en-AU")
APPLY = "--apply" in sys.argv


def slot_block_ids(doc: dict) -> set[str]:
    """本站结构槽位绑定的块 ID 集合（header / footer / 其余槽位）。"""
    st = (doc.get("settings") or {}).get("structure") or {}
    ids: set[str] = set()
    for key in ("headerBlockId", "footerBlockId"):
        v = str(st.get(key) or "").strip()
        if v:
            ids.add(v)
    for v in (st.get("slots") or {}).values():
        v = str(v or "").strip()
        if v:
            ids.add(v)
    return ids


def strip_slot_refs(nodes: list, ids: set[str], removed: list) -> list:
    """递归删除引用槽位块的 core.globalref 节点，返回保留下来的节点。"""
    out = []
    for n in nodes or []:
        props = n.get("props") or {}
        if n.get("type") == "core.globalref" and str(props.get("blockId") or "").strip() in ids:
            removed.append(str(n.get("id") or "(未命名)"))
            continue
        kids = n.get("children")
        if isinstance(kids, list) and kids:
            n["children"] = strip_slot_refs(kids, ids, removed)
        out.append(n)
    return out


def main() -> int:
    C.login()
    project = C.project_id()
    pages = C.load_map("pages.json")
    if not pages:
        print("没有页面清单（tmp/wp-import/map/pages.json），跳过")
        return 0

    total = 0
    for key, p in pages.items():
        code, body = C.api(f"/api/page/detail?projectId={project}&id={p['id']}", None, method="GET")
        detail = (body or {}).get("data") or {}
        doc = detail.get("draftDocument") or {}
        if not doc:
            print(f"  {key}: 读不到草稿文档（http={code}），跳过")
            continue
        ids = slot_block_ids(doc)
        if not ids:
            continue
        removed: list[str] = []
        doc["root"] = strip_slot_refs(doc.get("root") or [], ids, removed)
        if not removed:
            continue
        total += len(removed)
        print(f"  {key}: 待摘除 {len(removed)} 个槽位引用节点 → {', '.join(removed)}")
        if not APPLY:
            continue
        try:
            C.ok("/api/page/draft/save", {
                "id": p["id"],
                "expectedVersion": detail.get("draftVersion", 0),
                "draftPath": detail.get("draftPath", p.get("path", "/")),
                "draftDocument": doc,
            })
            # 与 nav_attach.py 同一条纪律：构建 / 发布必须显式带上工程默认语言，
            # 否则会落到 zh-CN 前缀路径上（根路径 404）。
            C.ok("/api/page/build", {"id": p["id"], "lang": DEFAULT_LANG})
            C.ok("/api/page/publish", {"id": p["id"], "lang": DEFAULT_LANG})
            print(f"  {key}: 已保存并重新发布")
        except Exception as e:  # noqa: BLE001
            print(f"  {key}: 保存/构建/发布失败 {str(e)[:200]}")
    print(f"共 {total} 个节点待处理" + ("（已应用）" if APPLY else "（预览模式，未写库；加 --apply 执行）"))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
