# market_match

A spot trading matching engine implemented from scratch in Go, modeled on the architecture of production exchange systems. It focuses on the heart of an exchange — the price–time priority matching loop, with exact decimal arithmetic and structured match results.

一个用 Go 从零复现的撮合引擎（边跑边学项目）。目标是吃透交易所最核心的部件：订单簿与撮合循环。

## 当前进度

- [x] 订单簿：红黑树（价格优先、时间优先）+ map 索引
- [x] 限价单撮合循环 `matchLimit`：taker/maker 回执、maker 定价
- [x] 第 4 层：补齐订单类型（Cancel / Market / CircuitRate / FOK）
- [ ] MySQL 拉单、结果落库、快照恢复、回放校验、行情派生

## 项目结构

| 文件 | 职责 |
|---|---|
| `order.go` | 订单结构、状态枚举、扣量推进、红黑树排序规则 |
| `orderbook.go` | 订单簿：两棵红黑树（买盘/卖盘）+ map 缓存 |
| `matcher.go` | 撮合循环与回执（OrderResult / MatchResult） |
| `main.go` | 验证场景 |

## 运行

```bash
go run .
```

## 订单类型 × 状态矩阵

每种类型的订单，一生能到达的状态集合不同：

| 类型 | 一生可能的状态 | 特点 |
|---|---|---|
| 限价 Limit | Submitted → PartialFilled → Filled / Canceled / PartialCanceled | 唯一有"漫长人生"的（可以挂在盘口等很久） |
| 市价 Market | 瞬间 → Filled / CircuitCanceled / Canceled（剩余作废） | 瞬时人生：进门就结算完，绝不留盘口 |
| FOK | Filled / Canceled | 更极端：要么全成，要么全不成，永远没有 partial |
| Cancel | （自己不是订单，是命令） | 它改的是**别人**的状态 |

记忆钩子：**从限价到 FOK，价格约束越来越松，成交承诺越来越硬**。

## 13 种订单状态全景

**活着的中间态（2 个）**——任何类型都可能经过：

```text
Submitted      挂着，还没成交
PartialFilled  成交了一部分，还剩着
```

**善终（1 个）**——唯一的圆满结局：

```text
Filled         全部成交
```

**人为死（4 个）**——由"人的决定"造成：

```text
Canceled             用户主动撤单；市价单剩余量作废也落这个
PartialCanceled      成交了一部分之后被撤掉剩下的
Failed               订单本身不合法，直接拒绝受理
Error / SystemCanceled  内部错误 / 系统风控强制撤
```

**规则死**——被撮合规则本身杀死：

```text
CircuitCanceled    熔断：市价单价格偏出首笔价 ×(1±rate)，剩余作废
PrecisionCanceled  精度不满足最小交易单位，被取消
SelfTradeCanceled / SelfTradePartialCanceled / SelfTradeDecreased  自成交保护
```

## 核心设计要点

- **永远不用 float 存钱**：价格与数量全部使用 `shopspring/decimal` 精确运算。
- **maker 定价**：每口成交价 = 挂单方（maker）的价格。
- **回执即归档**：`OrderResult` 一旦生成就只读不改；taker 的回执在循环结束时才生成（吃了几个 maker 此刻才确定），maker 的回执当场生成。
- **挂起的本质是"带价格约束地等待"**：市价单没有价格约束，所以流动性耗尽时只能作废，绝不挂单。

## 为什么订单先落库，而不是客户端直发撮合引擎？

真实链路：客户端 → 网关（校验、冻结资金）→ **MySQL 序列表** → 撮合引擎轮询拉单。撮合引擎是表的消费者，不是客户端的直接对接方。中间隔这层数据库，有三个理由：

1. **数据库是唯一可信的账本**。下单是资金操作，必须先持久化在案、事务成功后才回复用户"下单成功"。引擎内存里的盘口只是账本的临时投影——引擎崩溃、重启、升级，订单都安然在表里，起来后从 `id >= 断点` 续拉，一笔不丢。
2. **自增 id 提供全局唯一顺序**。多个用户"同时"下单，网络世界里先后无定义；写进同一张表，id 就排好了唯一全序——撮合的正确性（价格优先、时间优先）正建立在此之上，`Order.SeqId` 的最终来源就是表 id。
3. **表是天然的削峰排队区**。行情暴涨时的下单风暴先在表里排队，撮合按自己的节奏消费；引擎永远按"每次最多 LIMIT 2000、隔几百毫秒轮询"的步调工作，不会被流量打死。

一句话：**先记账、有全序、能排队——持久队列是撮合正确性与可靠性的前提**。
