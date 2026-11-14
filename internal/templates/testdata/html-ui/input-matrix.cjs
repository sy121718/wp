const assert = require('node:assert/strict');

// 只驱动真实输入，不注入 tabindex、焦点或滚动位置。
async function tabTo(page, selector) {
    for (let count = 1; count <= 180; count++) {
        await page.keyboard.press('Tab');
        if (await page.evaluate(s => document.activeElement?.matches(s), selector)) return count;
    }
    throw new Error('原生 Tab 无法到达：' + selector);
}

async function geometry(page, selector) {
    return page.$eval(selector, node => {
        let scroller = node.parentElement;
        while (scroller && !(scroller.scrollHeight > scroller.clientHeight + 1 && /auto|scroll/.test(getComputedStyle(scroller).overflowY))) {
            scroller = scroller.parentElement;
        }
        scroller = scroller || document.scrollingElement;
        const box = node.getBoundingClientRect();
        const area = scroller === document.scrollingElement ? { top: 0, bottom: innerHeight, left: 0, right: innerWidth } : scroller.getBoundingClientRect();
        const top = Math.max(0, area.top), bottom = Math.min(innerHeight, area.bottom);
        const left = Math.max(0, area.left), right = Math.min(innerWidth, area.right);
        return { visible: box.top >= top && box.bottom <= bottom && box.left >= left && box.right <= right,
            scrollTop: scroller.scrollTop, max: scroller.scrollHeight - scroller.clientHeight,
            x: (left + right) / 2, y: top + (bottom - top) * .75, height: bottom - top };
    });
}

async function scrollTo(page, selector, input, t) {
    const initial = await geometry(page, selector);
    assert.ok(initial.max > 0 || initial.visible, '无可滚动空间时按钮必须已经完整可见');
    const session = input === 'touch' ? await page.createCDPSession() : null;
    try {
        for (let n = 0; n < 35; n++) {
            const state = await geometry(page, selector);
            if (n > 0 && state.visible && (initial.max === 0 || state.scrollTop > initial.scrollTop)) break;
            if (input === 'wheel') {
                await page.mouse.move(state.x, state.y);
                await page.mouse.wheel({ deltaY: Math.min(240, state.height / 2) });
            } else {
                await session.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: state.x, y: state.y }] });
                for (let step = 1; step <= 6; step++) {
                    await session.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: state.x, y: state.y - Math.min(240, state.height / 2) * step / 6 }] });
                    await new Promise(resolve => setTimeout(resolve, 25));
                }
                await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
            }
            await new Promise(resolve => setTimeout(resolve, 150));
        }
    } finally { if (session) await session.detach(); }
    const final = await geometry(page, selector);
    if (initial.max > 0) {
        assert.ok(final.scrollTop > initial.scrollTop, `${input} 必须实际滚动：${initial.scrollTop} -> ${final.scrollTop}`);
    } else {
        assert.equal(final.scrollTop, 0, '内容完整容纳时不制造假滚动');
    }
    assert.ok(final.visible, `${input} 后按钮必须在真实滚动容器及视口内`);
    t.diagnostic(`${input}: scrollTop ${initial.scrollTop} -> ${final.scrollTop}${initial.max === 0 ? '（内容完整可见，无滚动空间）' : ''}`);
}

async function activate(page, selector, input, t) {
    if (input === 'keyboard') {
        const count = await tabTo(page, selector);
        assert.ok((await geometry(page, selector)).visible, '原生 Tab 后按钮必须可见');
        t.diagnostic(`keyboard: ${count} 次 Tab 到达 ${selector}`);
        await page.keyboard.press('Enter');
    } else {
        if (input === 'wheel' || input === 'touch') await scrollTo(page, selector, input, t);
        if (input === 'touch') await page.tap(selector);
        else await page.click(selector);
    }
}

module.exports = { tabTo, geometry, activate };
