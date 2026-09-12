/**
 * sky-enhance.js — 构建产物客户端增强（纯客户端交互，静态站运行）。
 * 零依赖 IIFE；按 data-* 属性按需初始化，无交互组件时静默跳过。
 * 覆盖：轮播（箭头/自动播放/循环/圆点高亮同步）、计数器（仅小数位模式——
 * 整数模式已由 @property + counter() 零 JS 实现）、倒计时（CSS 无时钟）。
 * 手风琴严格单开已改 <details name> 原生互斥；圆点导航已改构建期锚点；
 * tabs（radio hack）与基础 accordion（details）原生零 JS。
 */
(function () {
    'use strict';
    function onReady(fn) {
        if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', fn);
        else fn();
    }

onReady(function () {
        // 每个增强各自独立执行：任何一个抛错都不该拖累其余。
        // 这不是防御性编程 —— 排在最前面的增强一旦抛错，后面的全部不会执行，
        // 表现为「组件看着正常但某个功能完全不生效」，且控制台外的排查成本很高。
        [
            initCounters, initSliders, initCarousels, initCountdowns, initLightboxes,
            initCardStacks, initCardDecks, initSlideStacks,
        ].forEach(function (fn) {
            try {
                fn();
            } catch (e) {
                // 单个增强失败不影响其余；真要排查时在控制台里单独调该函数即可。
            }
        });
    });
})();