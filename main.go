package main

import (
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"
)

// 第 3 层完成标志场景：
// 盘口 3 个卖单 → 大买单吃 3 口 → 残余 2 个挂进买盘
func main() {
	book := NewOrderBook()

	// 盘口先挂 3 个卖单（价格递增，各 1 个）
	book.Enqueue(mkOrder(1, 1, 100, Sell, "50000", "1"))
	book.Enqueue(mkOrder(2, 2, 101, Sell, "50001", "1"))
	book.Enqueue(mkOrder(3, 3, 102, Sell, "50002", "1"))

	// 大买单来了：5 个 @ 50002
	taker := mkOrder(4, 10, 200, Buy, "50002", "5")
	price, results := book.matchLimit(taker)

	mr := MatchResult{OrderId: taker.OrderId, Price: price, Items: results}
	resultJSON, _ := json.MarshalIndent(mr, "", "  ")
	fmt.Println("===== 撮合回执 =====")
	fmt.Println(string(resultJSON))

	fmt.Println("===== 最终盘口 =====")
	fmt.Println("买盘：")
	for _, v := range book.BuySet.Values() {
		o := v.(*Order)
		fmt.Printf("  #%d user%d @ %v 剩 %v [%s]\n", o.OrderId, o.UserId, o.Price, o.UnfilledAmount, o.State)
	}
	fmt.Println("卖盘：")
	for _, v := range book.SellSet.Values() {
		o := v.(*Order)
		fmt.Printf("  #%d user%d @ %v 剩 %v [%s]\n", o.OrderId, o.UserId, o.Price, o.UnfilledAmount, o.State)
	}
}

// 小工具：少打点字。State 初始都是 Submitted
func mkOrder(seqId, orderId, userId int64, side BuyOrSell, price, amount string) *Order {
	p, _ := decimal.NewFromString(price)
	a, _ := decimal.NewFromString(amount)
	return &Order{
		SeqId:          seqId,
		UserId:         userId,
		OrderId:        orderId,
		BuyOrSell:      side,
		State:          Submitted,
		Price:          p,
		UnfilledAmount: a,
	}
}
