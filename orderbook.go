package main

import (
	"github.com/emirpasic/gods/sets/treeset"
)

// ===== 订单簿：map 管查找，两棵红黑树管排序 =====
type OrderBook struct {
	cache   map[int64]*Order // 按 OrderId 查（O(1)）
	BuySet  *treeset.Set     // 买盘口（自动排序）
	SellSet *treeset.Set     // 卖盘口
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		cache:   make(map[int64]*Order),
		BuySet:  treeset.NewWith(Comparator),
		SellSet: treeset.NewWith(Comparator),
	}
}

// 挂单：同时写 cache 和 TreeSet
func (book *OrderBook) Enqueue(order *Order) {
	book.cache[order.OrderId] = order
	if order.BuyOrSell == Buy {
		book.BuySet.Add(order)
	} else {
		book.SellSet.Add(order)
	}
}

// 撤单/被吃光：同时从 cache 和 TreeSet 删
func (book *OrderBook) Dequeue(orderId int64) {
	order, ok := book.cache[orderId]
	if !ok {
		return
	}
	delete(book.cache, orderId)
	if order.BuyOrSell == Buy {
		book.BuySet.Remove(order)
	} else {
		book.SellSet.Remove(order)
	}
}

// 按 orderId 查单（O(1)）
func (book *OrderBook) Find(orderId int64) *Order {
	return book.cache[orderId]
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
