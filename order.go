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

// ===== 订单状态 =====
// 第 3 层只需要 3 个，第 4 层会补齐到原版的 13 个
type OrderState int

const (
	Submitted    OrderState = iota // 初始状态：挂进盘口，还没成交过
	PartialFilled                  // 成交了一部分，还剩一些
	Filled                         // 全部成交
)

// 状态转字符串：JSON 回执里用这些字符串（对齐原版命名）
func (state OrderState) String() string {
	switch state {
	case Submitted:
		return "submitted"
	case PartialFilled:
		return "partial-filled"
	case Filled:
		return "filled"
	default:
		return "unknown"
	}
}

// ===== 订单 =====
type Order struct {
	SeqId          int64 // 全局排队号：同价时先来后到
	UserId         int64 // 下单用户 id：回执里要带上
	OrderId        int64
	BuyOrSell      BuyOrSell
	State          OrderState // 撮合过程中由 fillAmount 推进，初始 Submitted
	Price          decimal.Decimal
	UnfilledAmount decimal.Decimal
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
