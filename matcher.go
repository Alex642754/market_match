package main

import (
	"github.com/shopspring/decimal"
)

// ===== 回执：一次撮合中某个订单的成绩单，写完即归档，只读不改 =====
// 指针 + omitempty：为 nil 的字段会从 JSON 里消失——
// taker 只有 unfilled-amount（还剩多少没吃到），maker 只有 filled-amount（本次被吃了多少）
type OrderResult struct {
	OrderId        int64            `json:"order-id"`
	UserId         int64            `json:"user-id"`
	Role           string           `json:"role"` // "taker" 主动吃单 / "maker" 挂单被吃
	Price          *decimal.Decimal `json:"price,omitempty"`
	UnfilledAmount *decimal.Decimal `json:"unfilled-amount,omitempty"` // taker 专属
	FilledAmount   *decimal.Decimal `json:"filled-amount,omitempty"`   // maker 专属
	State          string           `json:"state"`
}

// 一次撮合的完整回执
// ★ 注意：items 里 taker 是【最后一条】，前面每条是一个 maker
//   下游都按 Role 字段区分角色，不按下标
type MatchResult struct {
	OrderId int64           `json:"order-id"` // taker 的订单号
	Price   decimal.Decimal `json:"price"`    // 本轮最后一次成交价（maker 定价）
	Items   []*OrderResult  `json:"items"`
}

// 价格交叉判断：买价 >= 卖价（或卖价 <= 买价）才能成交
func matchAble(order *Order, oppoOrder *Order) bool {
	if order.BuyOrSell == Buy {
		return order.Price.GreaterThanOrEqual(oppoOrder.Price)
	}
	return order.Price.LessThanOrEqual(oppoOrder.Price)
}

// ===== 限价单撮合循环（第 3 层的核心）=====
// 逻辑：吃对手盘最优单 → 吃到吃不动 → 吃不完的剩余量挂进盘口
func (book *OrderBook) matchLimit(order *Order) (price decimal.Decimal, results []*OrderResult) {
	for {
		if order.isFilled() {
			// taker 吃满了：它的回执最后才能开（吃了几个 maker，此刻才确定）
			results = append(results, finalizeTaker(order))
			break
		}

		oppoOrder := book.peekOppoOrder(order)
		if oppoOrder == nil || !matchAble(order, oppoOrder) {
			// 对手没货或价格交叉不了：剩余量挂进盘口，收工
			book.Enqueue(order)
			results = append(results, finalizeTaker(order))
			break
		}

		// 吃一口：成交量 = min(自己剩的, 对手剩的)，双方扣量，maker 当场开回执
		results = append(results, matchOrder(order, oppoOrder))
		if oppoOrder.isFilled() {
			book.Dequeue(oppoOrder.OrderId) // maker 被吃光，移出盘口
		}
		price = oppoOrder.Price // maker 定价：这一口的成交价是挂单方的价格
	}
	return price, results
}

// 吃一口：双方扣量 + 给 maker 开回执
func matchOrder(order *Order, oppoOrder *Order) *OrderResult {
	matchAmount := decimal.Min(order.UnfilledAmount, oppoOrder.UnfilledAmount)
	order.fillAmount(matchAmount)
	oppoOrder.fillAmount(matchAmount)

	return &OrderResult{
		OrderId:      oppoOrder.OrderId,
		UserId:       oppoOrder.UserId,
		Role:         "maker",
		Price:        &oppoOrder.Price,
		FilledAmount: &matchAmount,
		State:        oppoOrder.State.String(),
	}
}

// taker 的回执：循环结束时生成
// （原版按订单类型拆成 finalizeTakerLimit 等多个函数，第 4 层再拆）
func finalizeTaker(order *Order) *OrderResult {
	return &OrderResult{
		OrderId:        order.OrderId,
		UserId:         order.UserId,
		Role:           "taker",
		UnfilledAmount: &order.UnfilledAmount,
		Price:          &order.Price, // 限价单：回执带自己的限价
		State:          order.State.String(),
	}
}
