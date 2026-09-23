#!/usr/bin/env python3
# rebuild_artifacts.py — 只 build+publish 全部页面（不动草稿文档）。
# 产物是构建期字节：组件 CSS/模板一变，已发布产物不会自动跟上，必须重发一遍。
# N.call 成功返回 data、失败抛 RuntimeError —— 失败即计入 failures，不中断整批。
import os, sys

sys.path.insert(0, os.path.dirname(__file__))
import nav_attach as N  # noqa: E402  # 复用其 call / DEFAULT_LANG / P

pages = N.C.load_map('pages.json')
fail = 0
for key, p in pages.items():
    for action in ('build', 'publish'):
        try:
            N.call('/api/page/' + action, {'id': p['id'], 'lang': N.DEFAULT_LANG})
        except Exception as e:
            print('  %s: %s 异常 %s' % (key, action, str(e)[:160]))
            fail += 1
    print('  %s: done (%s)' % (key, p.get('path', '?')))
print('failures:', fail)
sys.exit(1 if fail else 0)
