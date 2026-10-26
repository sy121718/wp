"""IPI 站点复刻 · 站点层：清空 → 工程 → 主题 → 站点设置。

用法：
  python3 scripts/wp-import/ipi_site.py            # 幂等：清空本工程内容后重建
  python3 scripts/wp-import/ipi_site.py --keep      # 只建不删（调试主题时用）

设计取舍：
  · 所有写操作走真实 HTTP 接口（与 scripts/wp-import/ 既有导入工具同一条路），
    这样跑一遍就等于把接口链路也验了一遍；
  · 「清空」= 删本工程全部页面 / 块 / 导航，并把激活主题的设置重置为 IPI tokens；
    工程本身保留（删除工程会连带清掉发布指针，重建代价更高）；
  · 主题只写 tokens（colors / typography / button / surface / motion）；
    结构绑定（header/footer 块）由 ipi_blocks.py 回填到 settings 顶层。
"""
from __future__ import annotations

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import common as C  # noqa: E402

STATE_DIR = os.path.join(C.ROOT, "tmp", "ipi")
STATE = os.path.join(STATE_DIR, "state.json")

PROJECT_NAME = "IPI"

# IPI 的设计令牌取自原始手稿（深空黑 + 紫粉霓虹 + Orbitron/Rajdhani）：
#   bg #05040A  surface #12101B  border #241C3A
#   primary #8B5CF6  secondary #A855F7  accent #E879F9  cyan #22D3EE
#   text #ECE9FB  muted #A99FC6  dim #6F6489
THEME_NAME = "IPI · 暗夜霓虹"
THEME_SETTINGS = {
    "colors": {
        "primary": "#8B5CF6",
        "secondary": "#A855F7",
        "accent": "#E879F9",
        "success": "#22D3EE",
        "warning": "#F5A524",
        "danger": "#EF4444",
        "text": "#ECE9FB",
        "heading": "#FFFFFF",
        "background": "#05040A",
        "surface": "#12101B",
        "border": "#241C3A",
    },
    "typography": {
        "heading": {
            "color": "#FFFFFF",
            "fontWeight": "800",
            "fontSize": "44px",
            "spacing": "14px",
            # 单家族名：主题校验走 CSS 值白名单，带引号/逗号的 font stack 会被判非法。
            # 兜底家族由构建期 CSS 的 var(--sky-font-heading) 消费方补。
            "fontFamily": "Orbitron",
        },
        "body": {
            "color": "#A99FC6",
            "fontSize": "16px",
            "lineHeight": "1.75",
            "fontFamily": "Rajdhani",
        },
        "link": {"color": "#E879F9", "hoverColor": "#FFFFFF", "underline": "hover"},
    },
    "button": {
        "background": "#8B5CF6",
        "color": "#FFFFFF",
        "hoverBackground": "#E879F9",
        "hoverColor": "#0B0714",
        "radius": "999px",
        "fontWeight": "700",
        "paddingY": "13px",
        "paddingX": "28px",
        "borderWidth": "1px",
        "borderStyle": "solid",
        "borderColor": "#8B5CF6",
        "shadow": "neon",
    },
    "surface": {
        "radius": "16px",
        "borderWidth": "1px",
        "borderColor": "#241C3A",
        "shadow": "lg",
    },
    "motion": {
        "transitionDuration": "220",
        "easing": "ease-out",
        # 原站是「滚动到哪，内容才出现」的 H5 叙事：开全站滚动显现 + 默认 fade-up 入场。
        # 关掉这条，全站立刻退回静态页面（CSS animation-timeline: view()，无 JS）。
        "scrollRevealDefault": True,
        "defaultEntrance": "fade-in-up",
        "reducedMotion": True,
    },
}

# 字体走自托管 @font-face（ipi_fonts.py 生成）：外链 <link> 在无网/慢网下字体不生效，
# 手稿的 Orbitron 特征是整站视觉基准，不能赌第三方时序。
FONT_LINK = ""

SITE_SETTINGS = {
    "siteName": "IPI",
    "siteDesc": "Infinite Play Innovation — True 8K magnetic-switch keyboards and esports mice.",
    "contactEmail": "support@ipi.game",
    "headScripts": FONT_LINK,
    "urlPatterns": {
        "article": "/blog/{slug}",
        "product": "/product/{slug}",
        "product_category": "/product-category/{slug}",
    },
}


def load_state() -> dict:
    if os.path.exists(STATE):
        with open(STATE, encoding="utf-8") as f:
            return json.load(f)
    return {}


def save_state(d: dict) -> None:
    os.makedirs(STATE_DIR, exist_ok=True)
    with open(STATE, "w", encoding="utf-8") as f:
        json.dump(d, f, ensure_ascii=False, indent=1)


def reset_contents(pid: str) -> None:
    """清空本工程的内容：页面 → 块 → 导航。顺序按引用方向（页面引用块）。"""
    pages = C.ok(f"/api/page/list?projectId={pid}", method="GET") or []
    for p in pages:
        C.ok("/api/page/delete", {"id": p["id"]})
    print(f"  删除页面 {len(pages)} 个")

    blocks = C.ok(f"/api/block/list?projectId={pid}", method="GET") or []
    for b in blocks:
        code, body = C.api("/api/block/delete", {"id": b["id"], "force": True})
        if body.get("code") != 200:
            print(f"  块 {b.get('name')} 删除失败: {str(body.get('message'))[:80]}")
    print(f"  删除块 {len(blocks)} 个")

    navs = C.ok(f"/api/navigation/list?projectId={pid}", method="GET") or []
    for n in navs:
        C.api("/api/navigation/delete", {"id": n["id"]})
    print(f"  删除导航 {len(navs)} 条")


def ensure_project() -> str:
    st = load_state()
    pid = st.get("projectId")
    projects = C.ok("/api/project/list", method="GET") or []
    if pid and any(p["id"] == pid for p in projects):
        return pid
    if projects:
        pid = projects[0]["id"]
    else:
        pid = C.ok("/api/project/create", {"name": PROJECT_NAME, "settings": {}})["id"]
    st["projectId"] = pid
    save_state(st)
    return pid


def reset_themes(pid: str) -> str:
    """把主题收敛为「一套激活主题 + IPI tokens」。

    非激活主题直接删；激活主题改名并覆盖设置 —— 不新建，避免 uq_themes_project_active
    （同工程单套激活）在多次重跑时累积孤儿主题。
    """
    themes = C.ok(f"/api/theme/list?projectId={pid}", method="GET") or []
    active = next((t for t in themes if t.get("isActive")), None)
    for t in themes:
        if active and t["id"] == active["id"]:
            continue
        C.api("/api/theme/delete", {"id": t["id"]})
    if not active:
        active = C.ok("/api/theme/create", {"projectId": pid, "name": THEME_NAME, "settings": {}})
    # tokens 整体覆盖，但结构槽位绑定（headerBlockId / footerBlockId / slots）必须留着：
    # 它们是另一层语义，被 tokens 重写顺手清掉的样子是「主题里配着页眉，站点却没有」。
    old = active.get("settings") or {}
    settings = dict(THEME_SETTINGS)
    for key in ("headerBlockId", "footerBlockId", "slots"):
        if old.get(key):
            settings[key] = old[key]
    return C.ok("/api/theme/update", {
        "id": active["id"], "name": THEME_NAME, "settings": settings,
    })["id"]


def update_site(pid: str) -> None:
    C.ok("/api/project/update", {"id": pid, "name": PROJECT_NAME, "settings": SITE_SETTINGS})


def main() -> None:
    keep = "--keep" in sys.argv
    C.login()
    pid = ensure_project()
    print("工程", pid)
    if not keep:
        reset_contents(pid)
    tid = reset_themes(pid)
    update_site(pid)
    st = load_state()
    st.update({"projectId": pid, "themeId": tid})
    save_state(st)
    print("主题", tid)
    print("完成")


if __name__ == "__main__":
    main()
