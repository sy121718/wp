// Package view 是页面出口与页面视图的**契约包**：不渲染、不持模板、不认识 gin。
//
// 定位（pkg/CLAUDE.md「契约三件」里的 view 一项）：它是控制器与渲染层之间的那一层接口，
// 由三个小契约组成，各自一个文件：
//
//	result.go    视图告诉出口「这一屏是 200 还是失败页」「要不要 no-store」
//	list.go      视图自报分页事实（第几页/每页多少/总共多少），出口据此放分页条
//	render.go    控制器唯一的出口接线：Render(c, view) / Fail(c, err)，模板名由装配点带
//	redirect.go  跳转出口（重定向是页面语义，与 JSON 出口分开）
//
// 依赖方向：internal/**(控制器、adminui、routers) → pkg/view → pkg/{response,facing}。
// 本包**不 import internal/**，也不 import 任何模板引擎。
//
// ## 为什么这里没有 Engine / driver
//
// 曾经有：`Engine`（持有 driver、提供 Render/RenderTo/Exists）+ `driver` 契约包 +
// `driver/jet` 实现。它是按 ThinkPHP think\View 的形态设计的「视图引擎门面」。
//
// 删掉的理由是**它从来没有被调用过一次**：`view.New(` 全仓 0 处调用，
// `driver/jet` 0 处 import，`RenderTo` / `Exists` 0 处使用 —— 全仓直出模板的路径是
// adminui.RenderPage → PrepareView → c.HTML，与本包无关。
//
// 更要紧的是它混淆了本包的定位：CLAUDE.md 把它写成「视图结果接口」，而 Engine 是一个
// **渲染入口**。两套渲染入口并存时，下一个人必须先判断「这个页面该走哪条」—— 而判断
// 依据只能靠读代码猜。删掉之后 pkg/view 的职责回到一句话：定义出口与视图之间的契约。
//
// 若将来真需要「可替换的模板引擎」，正确的位置不是复活这个门面，而是让
// adminui.RenderPage 变成一个可注入的实现 —— 那时需要的是**一个**入口的参数化，
// 不是**两个**入口并存。
package view
