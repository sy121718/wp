package navigationmodel

// navigation_errors.go — model 层的结构性错误（不是业务错误）。
//
// 为什么需要一条：`ListTx` / `Transaction` 在没有事务句柄时只能返回「用法错误」。
// 它不该是一条业务错误（那会被出口当 4xx 展示给用户），也不该是 nil（那会让调用方
// 在「什么都没读到」的前提下继续往下写）。这里给一条**普通 error**：
// 出去以后由 service 归口成 500（装配/编排缺陷，重试无用）。
//
// 层归属：model —— 只表达「调用方式不成立」，不含业务语义、不取词、不带状态码。

import "errors"

// ErrTransactionRequired 需要事务句柄的入口被以非事务方式调用。
var ErrTransactionRequired = errors.New("navigationmodel: 该入口必须在调用方事务内调用")
