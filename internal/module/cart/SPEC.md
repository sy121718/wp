---
id: go-wp-mod-cart
type: module-design
title: cart — 购物车
status: active
parent: go-wp-modules
---

# cart — 购物车

## 职责

购物车状态与结算入口。购物车**没有表**：状态在客户端签名 cookie 里，服务端只校验与重算。

## 边界

- **不建订单、不落账**：下单的唯一入口是 `order`；本模块只在结算时调用它。
- 价格与可用量经 `product` 契约取快照；会员折扣经 `membership` 契约读。
- 支付通道以 `PaymentGateway` 接口注入，缺注入即视为权益/通道关闭。
- 不持有支付通道清单以外的东西。

## 依赖

`contract` → `product`、`membership`、`order`、`project`。

`membership/dto` 是已知的跨模块 dto 引用（`service/cart.go`），未登记进 `module_boundary_test.go` 的债务清单——「跨模块只经 contract」的判据在这里没有守住。
