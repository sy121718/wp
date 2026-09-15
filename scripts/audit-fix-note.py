#!/usr/bin/env python3
"""覆盖单条 finding 的 resolutionNote（用于「已完成但结论后来被修正」的条目）。"""
import json
import glob
import os

ROOT = os.path.join(os.path.dirname(__file__), "..", "docs", "audit")

OVERRIDES = {
    "TX-007": (
        "2026-09-14 修正：改由入口的 request_id 幂等（RegisterReceipt 命中既有入库单直接重放）"
        "保证「不会重复加库存」。原先的「按采购单号查流水判重」已移除 —— 一张采购单分多次收货"
        "是常态（部分到货），按单号判重会把第二批误判成重复：库存不加、单据状态却推进，"
        "账实不符且没有任何报错。"
    ),
}


def main() -> None:
    changed = 0
    for path in sorted(glob.glob(os.path.join(ROOT, "dimensions", "*.json"))):
        with open(path, "r", encoding="utf-8") as fh:
            data = json.load(fh)
        touched = False
        for finding in data.get("findings", []):
            note = OVERRIDES.get(finding.get("id"))
            if note is None or finding.get("resolutionNote") == note:
                continue
            finding["resolutionNote"] = note
            touched = True
        if touched:
            with open(path, "w", encoding="utf-8") as fh:
                json.dump(data, fh, ensure_ascii=False, indent=2)
                fh.write("\n")
            print(f"{os.path.basename(path)}: note updated")
            changed += 1
    print(f"files updated: {changed}")


if __name__ == "__main__":
    main()
