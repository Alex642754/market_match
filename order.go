package main

import (
	"github.com/shopspring/decimal"
)

// ===== 买卖方向（iota 枚举：Buy=0, Sell=1）=====
type BuyOrSell int

const (
	Buy BuyOrSell = iota
	Sell
)

// ===== 订单类型：分诊台按它决定走哪条路 =====
type OrderType int

const (
	Limit  OrderType = iota // 限价：吃不动就挂单等
	Market                   // 市价：吃到吃不动，剩余作废
	Fok                      // 全成或全不成：先预检流动性，够才走市价流程
	Cancel                   // 撤单命令：不是订单，改的是别人的状态
)

func (t OrderType) String() string {
	switch t {
	case Market:
		return "market"
	case Fok:
		return "fok"
	case Cancel:
		return "cancel"
	default:
		return "limit"
	}
}

// ===== 13 种订单状态（对齐原版枚举）=====
type OrderState int

const (
	Submitted                OrderState = iota // 挂着，还没成交
	PartialFilled                              // 成交了一部分，还剩着
	PartialCanceled                            // 成交一部分后被撤
	Canceled                                   // 撤销/作废（一口没吃）
	Filled                                     // 全部成交
	Failed                                     // 订单不合法，拒绝受理
	Error                                      // 内部错误
	CircuitCanceled                            // 熔断：价格偏出首笔价×(1±rate)
	SystemCanceled                             // 系统风控强制撤
	SelfTradeCanceled                          // 自成交保护：整单撤
	SelfTradePartialCanceled                   // 自成交保护：部分撤
	SelfTradeDecreased                         // 自成交保护：减量
	PrecisionCanceled                          // 剩余量不满足最小精度，被取消
)

// 状态转字符串：对外回执用这些词汇（对齐原版命名）
var stateStr = map[OrderState]string{
	Submitted:                "submitted",
	PartialFilled:            "partial-filled",
	PartialCanceled:          "partial-canceled",
	Canceled:                 "canceled",
	Filled:                   "filled",
	Failed:                   "failed",
	Error:                    "error",
	CircuitCanceled:          "circuit-canceled",
	SystemCanceled:           "system-canceled",
	SelfTradeCanceled:        "self-trade-canceled",
	SelfTradePartialCanceled: "self-trade-partial-canceled",
	SelfTradeDecreased:       "self-trade-decreased",
	PrecisionCanceled:        "precision-canceled",
}

func (state OrderState) String() string {
	if s, ok := stateStr[state]; ok {
		return s
	}
	return "unknown"
}

// ===== 订单 =====
type Order struct {
	SeqId          int64 // 全局排队号：同价时先来后到
	UserId         int64 // 下单用户 id：回执里要带上
	OrderId        int64
	Type           OrderType // ★ 第 4 层新增：订单类型
	BuyOrSell      BuyOrSell
	State          OrderState // 撮合过程中由扣量推进，初始 Submitted
	Price          decimal.Decimal
	CircuitRate    decimal.Decimal // ★ 新增：熔断比例，0 表示不启用
	UnfilledAmount decimal.Decimal // ★ 注意：市价买单存的是"钱"，其他存"币"
}

// 扣量：成交 amount 后更新剩余量，状态在这里顺带推进
// ★ 状态不靠外部猜，而是从"扣量"这个唯一入口推进——这是原版的思路
func (order *Order) fillAmount(amount decimal.Decimal) {
	order.UnfilledAmount = order.UnfilledAmount.Sub(amount)
	if order.UnfilledAmount.Equal(decimal.Zero) {
		order.State = Filled
	} else {
		order.State = PartialFilled
	}
}

func (order *Order) isFilled() bool {
	return order.State == Filled
}

func (order *Order) isPartialFilled() bool {
	return order.State == PartialFilled
}

// 对手方向：买单的对手在卖盘，卖单的对手在买盘
func (order *Order) oppoBuyOrSell() BuyOrSell {
	if order.BuyOrSell == Buy {
		return Sell
	}
	return Buy
}

// ===== 红黑树的排队规则 =====
// 返回 -1 表示 a 排在 b 前面，1 表示排在后面，0 表示同一笔
// 买盘：价高优先；卖盘：价低优先；同价：SeqId 小的（先来的）优先
func Comparator(a, b interface{}) int {
	oa := a.(*Order)
	ob := b.(*Order)

	if oa.BuyOrSell == Buy {
		switch {
		case oa.Price.GreaterThan(ob.Price) ||
			(oa.Price.Equal(ob.Price) && oa.SeqId < ob.SeqId):
			return -1
		case oa.SeqId == ob.SeqId:
			return 0
		default:
			return 1
		}
	} else {
		switch {
		case oa.Price.LessThan(ob.Price) ||
			(oa.Price.Equal(ob.Price) && oa.SeqId < ob.SeqId):
			return -1
		case oa.SeqId == ob.SeqId:
			return 0
		default:
			return 1
		}
	}
}
