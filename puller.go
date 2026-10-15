package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // 注册 mysql 驱动，下划线 = 只用它的 init()
)

// ===== 表里 type 列的编码（对齐原版 puller 常量）=====
// 一个数字同时编码了「类型 + 方向」，解析时拆开
const (
	tcCancel     int32 = 0 // 撤单命令
	tcBuyMarket  int32 = 1
	tcSellMarket int32 = 2
	tcBuyLimit   int32 = 3
	tcSellLimit  int32 = 4
	tcBuyFok     int32 = 7
	tcSellFok    int32 = 8
)

// 编码 → Order 的 Type / BuyOrSell
// 撤单命令没有方向，占位填 Buy（matchCancel 按 OrderId 找目标，方向根本用不上）
func decodeType(t int32, order *Order) {
	switch t {
	case tcCancel:
		order.Type, order.BuyOrSell = Cancel, Buy
	case tcBuyMarket:
		order.Type, order.BuyOrSell = Market, Buy
	case tcSellMarket:
		order.Type, order.BuyOrSell = Market, Sell
	case tcBuyLimit:
		order.Type, order.BuyOrSell = Limit, Buy
	case tcSellLimit:
		order.Type, order.BuyOrSell = Limit, Sell
	case tcBuyFok:
		order.Type, order.BuyOrSell = Fok, Buy
	case tcSellFok:
		order.Type, order.BuyOrSell = Fok, Sell
	default:
		order.Type, order.BuyOrSell = Limit, Buy
	}
}

// ===== 拉单器（生产者 goroutine）=====
// 死循环：查库 → 转成 Order → 推进 channel → 推进断点 → 没货就睡 300ms
// fromId 是断点（追剧进度条）：本次从它开始拉，拉完更新为最后一条的 id+1
func GoPuller(dsn, table string, ch chan *Order, fromId int64) {
	go func() {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			panic(err)
		}
		// Prepare 一次，反复用：省去每次拼 SQL 的解析开销（原版同款写法）
		stmt, err := db.Prepare(fmt.Sprintf(
			"SELECT id, type, order_id, amount, price, circuit_rate, user_id "+
				"FROM %s WHERE id >= ? ORDER BY id ASC LIMIT 2000", table))
		if err != nil {
			panic(err)
		}
		defer stmt.Close()

		for {
			rows, err := stmt.Query(fromId)
			if err != nil {
				fmt.Println("拉单查询出错，1s 后重试:", err)
				time.Sleep(time.Second)
				continue
			}

			var last int64 = -1
			for rows.Next() {
				var t int32
				order := &Order{State: Submitted} // 从库里来的单，人生从 Submitted 起步
				// decimal.Decimal 实现了 sql.Scanner，DECIMAL 列可以直接 Scan 进来
				if err := rows.Scan(&order.SeqId, &t, &order.OrderId, &order.UnfilledAmount,
					&order.Price, &order.CircuitRate, &order.UserId); err != nil {
					fmt.Println("行扫描出错，跳过:", err)
					continue
				}
				decodeType(t, order)
				ch <- order // 放上传送带；带满了(5000)会阻塞在这里，自动背压
				last = order.SeqId
			}
			rows.Close()

			if last >= 0 {
				fromId = last + 1 // 断点推进：下一轮从最后一条的下一 id 开始
				// ★ 拉到了货就不睡觉：说明可能有积压，立刻再查
			} else {
				time.Sleep(300 * time.Millisecond) // 没新货：睡 300ms 省资源
			}
		}
	}()
}
