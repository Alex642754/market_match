package main

import (
	"time"

	"github.com/shopspring/decimal"
)

// ===== 回执：一次撮合中某个订单的成绩单，写完即归档，只读不改 =====
// 指针 + omitempty：为 nil 的字段会从 JSON 里消失——
// taker 只有 unfilled-amount（还剩多少没吃到），maker 只有 filled-amount（本次被吃了多少）
type OrderResult struct {
	OrderId        int64            `json:"order-id"`
	UserId         int64            `json:"user-id"`
	Role           string           `json:"role"` // "taker" 主动方 / "maker" 挂单方
	Price          *decimal.Decimal `json:"price,omitempty"`
	UnfilledAmount *decimal.Decimal `json:"unfilled-amount,omitempty"`
	FilledAmount   *decimal.Decimal `json:"filled-amount,omitempty"`
	State          string           `json:"state"`
}

// 一次撮合的完整回执。items 里 taker 是最后一条，前面每条是一个 maker；
// 下游都按 Role 字段区分角色，不按下标
type MatchResult struct {
	Id      int64           `json:"f-id"`  // ★ 第 6 层新增：确定性主键 = taker SeqId，重撮合也不变
	Ts      int64           `json:"ts"`    // ★ 第 6 层新增：毫秒时间戳，落库用
	OrderId int64           `json:"order-id"`
	Price   decimal.Decimal `json:"price"` // 本轮最后一次成交价（maker 定价）
	Items   []*OrderResult  `json:"items"`
}

// ===== 分诊台：所有订单的唯一入口，按类型分发 =====
// ★ 统一在出口填 Id / Ts：Id = 输入 SeqId（确定性！重撮合也不变，幂等落库的前提）
func (book *OrderBook) match(order *Order) MatchResult {
	var mr MatchResult
	switch order.Type {
	case Cancel:
		mr = book.matchCancel(order)
	case Fok:
		mr = book.matchFok(order)
	case Market:
		price, items := book.matchMarket(order)
		mr = MatchResult{OrderId: order.OrderId, Price: price, Items: items}
	default: // Limit
		price, items := book.matchLimit(order)
		mr = MatchResult{OrderId: order.OrderId, Price: price, Items: items}
	}
	mr.Id = order.SeqId              // 确定性 f_id
	mr.Ts = time.Now().UnixMilli()   // 落库时间戳（非确定性，只用于排序/查询）
	return mr
}

// 价格交叉判断：买价 >= 卖价（或卖价 <= 买价）才能成交（限价单专用）
func matchAble(order *Order, oppoOrder *Order) bool {
	if order.BuyOrSell == Buy {
		return order.Price.GreaterThanOrEqual(oppoOrder.Price)
	}
	return order.Price.LessThanOrEqual(oppoOrder.Price)
}

// ===== 限价单撮合循环 =====
// 逻辑：吃对手盘最优单 → 吃到吃不动 → 吃不完的剩余量挂进盘口
func (book *OrderBook) matchLimit(order *Order) (price decimal.Decimal, results []*OrderResult) {
	for {
		if order.isFilled() {
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

// ===== 市价单撮合循环 =====
// 和限价单唯一的区别：没有价格约束（不用 matchAble）+ 吃不动时剩余量作废（不 Enqueue）
func (book *OrderBook) matchMarket(order *Order) (price decimal.Decimal, results []*OrderResult) {
	for {
		if order.isFilled() {
			results = append(results, finalizeTaker(order))
			break
		}

		oppoOrder := book.peekOppoOrder(order)
		if oppoOrder == nil {
			// 盘口空了：市价单绝不挂单，剩余量作废
			results = append(results, finalizeTaker(order))
			break
		}

		// 熔断检查：已有成交 && 启用了 rate && 下一口价格偏出 首口价×(1±rate)
		if len(results) > 0 && !order.CircuitRate.Equal(decimal.Zero) {
			first := *results[0].Price // 首口成交价 = 第一个 maker 的价格
			low := decimal.NewFromInt(1).Sub(order.CircuitRate).Mul(first)
			high := decimal.NewFromInt(1).Add(order.CircuitRate).Mul(first)
			if oppoOrder.Price.LessThanOrEqual(low) || oppoOrder.Price.GreaterThanOrEqual(high) {
				order.State = CircuitCanceled
				results = append(results, finalizeTaker(order))
				break
			}
		}

		// 吃一口（matchOrder 返回 nil = 剩余的钱买不起最小精度，精度死亡）
		r := matchOrder(order, oppoOrder)
		if r == nil {
			results = append(results, finalizeTaker(order))
			break
		}
		results = append(results, r)

		if oppoOrder.isFilled() {
			book.Dequeue(oppoOrder.OrderId)
		}
		price = oppoOrder.Price
	}
	return price, results
}

// ===== FOK：先看后吃 =====
// 预检对手盘总流动性：够 → 走市价流程（保证吃满）；不够 → 整单取消，盘口一根手指都不碰
func (book *OrderBook) matchFok(order *Order) MatchResult {
	if !book.canFillFok(order) {
		return MatchResult{OrderId: order.OrderId, Items: []*OrderResult{{
			OrderId:        order.OrderId,
			UserId:         order.UserId,
			Role:           "taker",
			UnfilledAmount: &order.UnfilledAmount,
			State:          Canceled.String(),
		}}}
	}
	// 预检通过才敢开吃；单线程撮合保证预检和执行之间没人插队
	price, items := book.matchMarket(order)
	return MatchResult{OrderId: order.OrderId, Price: price, Items: items}
}

// 扫一遍对手盘累加剩余量，够吃返回 true（FOK 的量按"币"算）
func (book *OrderBook) canFillFok(order *Order) bool {
	set := book.SellSet
	if order.BuyOrSell == Sell {
		set = book.BuySet
	}
	sum := decimal.Zero
	it := set.Iterator()
	for it.Next() {
		sum = sum.Add(it.Value().(*Order).UnfilledAmount)
		if sum.GreaterThanOrEqual(order.UnfilledAmount) {
			return true
		}
	}
	return false
}

// ===== 撤单命令 =====
// 按 orderId 从盘口删；撤不到返回 failed。撤单不改别人的人生，只终结它
func (book *OrderBook) matchCancel(cmd *Order) MatchResult {
	target := book.Find(cmd.OrderId)
	if target == nil {
		return MatchResult{OrderId: cmd.OrderId, Items: []*OrderResult{{
			OrderId: cmd.OrderId,
			Role:    "taker",
			State:   Failed.String(),
		}}}
	}

	state := Canceled
	if target.isPartialFilled() {
		state = PartialCanceled // 成交过一部分的被撤，要如实区分
	}
	target.State = state
	book.Dequeue(target.OrderId)

	return MatchResult{OrderId: target.OrderId, Items: []*OrderResult{{
		OrderId:        target.OrderId,
		UserId:         target.UserId,
		Role:           "taker",
		UnfilledAmount: &target.UnfilledAmount,
		State:          state.String(),
	}}}
}

// 吃一口：双方扣量 + 给 maker 开回执
// 返回 nil 表示市价买遇到精度死亡（剩余的钱买不起最小单位）
func matchOrder(order *Order, oppoOrder *Order) *OrderResult {
	matchAmount := decimal.Min(order.UnfilledAmount, oppoOrder.UnfilledAmount)

	if order.Type == Market && order.BuyOrSell == Buy {
		// ★ 市价买：自己账上记的是"钱"，先按对手价折算成能买的"币"
		//   向下截断 8 位小数（类似"聪"的最小单位），防止花出去的钱超过账上的钱
		affordable := order.UnfilledAmount.Div(oppoOrder.Price).Truncate(8)
		if affordable.LessThanOrEqual(decimal.Zero) {
			order.State = PrecisionCanceled
			return nil
		}
		matchAmount = decimal.Min(affordable, oppoOrder.UnfilledAmount)
		order.UnfilledAmount = order.UnfilledAmount.Sub(matchAmount.Mul(oppoOrder.Price)) // 扣的是钱
		if order.UnfilledAmount.Equal(decimal.Zero) {
			order.State = Filled
		} else {
			order.State = PartialFilled
		}
	} else {
		order.fillAmount(matchAmount) // 其他类型：扣的是币
	}
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

// taker 的回执：循环结束时生成，按订单类型做"状态翻译"
func finalizeTaker(order *Order) *OrderResult {
	r := &OrderResult{
		OrderId:        order.OrderId,
		UserId:         order.UserId,
		Role:           "taker",
		UnfilledAmount: &order.UnfilledAmount,
	}

	switch order.Type {
	case Market, Fok:
		// 翻译官：内部状态 → 对外业务词汇
		// （PartialFilled 在市价单嘴里是"partial-canceled"：吃了一部分后没得吃了）
		switch order.State {
		case Submitted:
			r.State = "canceled" // 一口没吃，整单作废
		case PartialFilled:
			r.State = "partial-canceled"
		case CircuitCanceled:
			r.State = "circuit-canceled"
		case PrecisionCanceled:
			r.State = "precision-canceled"
		default:
			r.State = order.State.String()
		}
	default: // Limit：如实报告，挂着的就是挂着的
		r.Price = &order.Price
		r.State = order.State.String()
	}
	return r
}
