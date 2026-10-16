package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// ===== 结果表名 =====
const resultTableName = "t_exchange_match_result_test"

// ===== 批量落库的攒批器（第二个生产者-消费者：撮合器是生产者，攒批器是消费者）=====
// 触发落库的两个条件（原版同款设计）：
//  ① 攒够 batchSize 条      → 立刻写（量触发）
//  ② ticker 到点（1 秒）     → 有多少写多少（时间触发，保证延迟上限）
// INSERT IGNORE 是幂等的关键：重撮合产生同样的 f_id 时静默跳过，不会脏表
func GoPersist(dsn string, ch chan MatchResult, batchSize int) {
	go func() {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			panic(err)
		}

		batch := make([]MatchResult, 0, batchSize)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		flush := func() {
			if len(batch) == 0 {
				return
			}
			// ★ 落库前按 f_id 排序（原版 insertSortMr）：
			//   B+ 树顺序写，数据库不用中途挪页；对账时按 f_id 读出来就是撮合顺序
			sort.Slice(batch, func(i, j int) bool { return batch[i].Id < batch[j].Id })

			values := make([]string, len(batch))
			for i, mr := range batch {
				role := mr.Items[len(mr.Items)-1].Role // taker 是最后一条
				orderType := "submit-cancel"
				if role != "cancel" {
					orderType = matchOrderTypeStr(mr)
				}
				mrBytes, _ := json.Marshal(mr)
				// 单条 VALUES：(f_id, ts, order_type, role, 'mr_json')
				values[i] = fmt.Sprintf("(%d,%d,'%s','%s','%s')",
					mr.Id, mr.Ts, orderType, role, escape(mrBytes))
			}
			prefix := fmt.Sprintf(
				"INSERT IGNORE INTO %s (f_id, ts, order_type, role, mr) VALUES ", resultTableName)
			fullSQL := prefix + values[0]
			for i := 1; i < len(values); i++ {
				fullSQL += "," + values[i]
			}
			if _, err := db.Exec(fullSQL); err != nil {
				fmt.Println("落库出错:", err)
			} else {
				fmt.Printf("  → 落库 %d 条 (f_id %d~%d)\n", len(batch), batch[0].Id, batch[len(batch)-1].Id)
			}
			batch = batch[:0] // 清空，复用底层数组
		}

		for {
			select {
			case mr := <-ch:
				batch = append(batch, mr)
				if len(batch) >= batchSize {
					flush()
				}
			case <-ticker.C:
				flush() // 到点就刷：保证延迟上限 1 秒，不等攒满
			}
		}
	}()
}

// MatchResult → order_type 字符串（简化版，够用就行）
// 原版从 mr.OrderTypeStr 取；我们没那个字段，从 mr.Items 里反推
func matchOrderTypeStr(mr MatchResult) string {
	if len(mr.Items) == 0 {
		return "unknown"
	}
	// taker 是最后一条
	taker := mr.Items[len(mr.Items)-1]
	// 从 role 区分 cancel / maker / taker，再简化为 direction-type
	if taker.Role == "maker" {
		return "maker"
	}
	return "match" // 简化：都是 taker，不分限价/市价/FOK（原版更细，本层够用）
}

// 转义 JSON 里的单引号和反斜杠：MySQL 字符串用单引号括
func escape(b []byte) string {
	s := string(b)
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			result = append(result, '\'', '\'')
		} else if s[i] == '\\' {
			result = append(result, '\\', '\\')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}
