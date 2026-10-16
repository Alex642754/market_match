package main

import (
	"github.com/emirpasic/gods/sets/treeset"
)

// ===== 订单簿：map 管查找，两棵红黑树管排序 =====
type OrderBook struct {
	FromId  int64           // ★ 第 7 层新增：快照断点 = 拉单器当前消费到的最大 SeqId
	Cache   map[int64]*Order // gob 需要公开字段才能编码；原版同款设计
	BuySet  *treeset.Set
	SellSet *treeset.Set
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		Cache:   make(map[int64]*Order),
		BuySet:  treeset.NewWith(Comparator),
		SellSet: treeset.NewWith(Comparator),
	}
}

// 挂单：同时写 Cache 和 TreeSet
func (book *OrderBook) Enqueue(order *Order) {
	book.Cache[order.OrderId] = order
	if order.BuyOrSell == Buy {
		book.BuySet.Add(order)
	} else {
		book.SellSet.Add(order)
	}
}

// 撤单/被吃光：同时从 Cache 和 TreeSet 删
func (book *OrderBook) Dequeue(orderId int64) {
	order, ok := book.Cache[orderId]
	if !ok {
		return
	}
	delete(book.Cache, orderId)
	if order.BuyOrSell == Buy {
		book.BuySet.Remove(order)
	} else {
		book.SellSet.Remove(order)
	}
}

// 按 orderId 查单（O(1)）
func (book *OrderBook) Find(orderId int64) *Order {
	return book.Cache[orderId]
}

// 看某一侧的最优单（买一/卖一），只看不拿
func (book *OrderBook) Peek(side BuyOrSell) *Order {
	var set *treeset.Set
	if side == Buy {
		set = book.BuySet
	} else {
		set = book.SellSet
	}
	it := set.Iterator()
	if it.First() {
		return it.Value().(*Order)
	}
	return nil
}

// 看对手盘最优单：taker 是买单就看卖一，是卖单就看买一
func (book *OrderBook) peekOppoOrder(order *Order) *Order {
	return book.Peek(order.oppoBuyOrSell())
}
