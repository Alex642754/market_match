package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/shopspring/decimal"
)

// DSN 从环境变量 DB_DSN 读取；没设置时用本地开发默认值。
// 生产代码里密码永远不硬编码进仓库（本仓库的默认值只是本地实验用）。
func getDSN() string {
	if d := os.Getenv("DB_DSN"); d != "" {
		return d
	}
	return "root:123456@tcp(127.0.0.1:3306)/market_match"
}

const tableName = "aibit_spot_sequence_test"

// ===== 场景用例结构 =====
type scenarioOrder struct {
	Id          int64  `json:"id"`
	User        int64  `json:"user"`
	Side        string `json:"side"` // "buy" / "sell"
	Type        string `json:"type"` // "limit" / "market" / "fok" / "cancel"
	Price       string `json:"price"`
	Amount      string `json:"amount"`
	CircuitRate string `json:"circuit-rate"`
}

type scenario struct {
	Name     string          `json:"name"`
	Orders   []scenarioOrder `json:"orders"`
	Expected []string        `json:"expected"`
}

// 回执 → 一行可读字符串，用来和 expected 逐条比对
func itemStr(r *OrderResult) string {
	s := fmt.Sprintf("%s #%d %s", r.Role, r.OrderId, r.State)
	if r.FilledAmount != nil {
		s += fmt.Sprintf(" filled=%v@%v", *r.FilledAmount, *r.Price)
	}
	if r.UnfilledAmount != nil {
		s += fmt.Sprintf(" unfilled=%v", *r.UnfilledAmount)
	}
	return s
}

// JSON 订单 → Order
func buildOrder(so scenarioOrder, seqId int64) *Order {
	var side BuyOrSell
	if so.Side == "sell" {
		side = Sell
	}
	var typ OrderType
	switch so.Type {
	case "market":
		typ = Market
	case "fok":
		typ = Fok
	case "cancel":
		typ = Cancel
	default:
		typ = Limit
	}
	price, _ := decimal.NewFromString(so.Price)
	amount, _ := decimal.NewFromString(so.Amount)
	circuitRate, _ := decimal.NewFromString(so.CircuitRate)

	return &Order{
		SeqId:          seqId,
		UserId:         so.User,
		OrderId:        so.Id,
		Type:           typ,
		BuyOrSell:      side,
		State:          Submitted,
		Price:          price,
		CircuitRate:    circuitRate,
		UnfilledAmount: amount,
	}
}

// ===== 入口 =====
// go run .        → 服务模式：拉单 goroutine + 撮合 goroutine，永远在线
// go run . test   → 场景模式：跑第 4 层的 12 道回归题
func main() {
	if len(os.Args) > 1 && os.Args[1] == "test" {
		runScenarios()
		return
	}
	runService()
}

// 快照目录
const snapshotDir = "./snapshots"

// 服务模式：两条流水线 + 定时快照
// ① 拉单器 → chan订单 → 撮合器（单线程碰盘口）
// ② 撮合器 → chan结果 → 攒批器（批量落库，幂等）
// ③ 定时 → 写快照（第 7 层，kill -9 后能恢复）
func runService() {
	// ★ 启动时先找最新快照，恢复盘口和断点
	book, savedFromId := LoadSnapshot(snapshotDir)
	pullFromId := savedFromId + 1 // 从快照断点的下一条开始拉（快照里的已撮合过）

	orderCh := make(chan *Order, 5000)
	resultCh := make(chan MatchResult, 5000)

	// 生产者①：拉单 goroutine（断点从恢复值开始，没有快照就是 1）
	go GoPuller(getDSN(), tableName, orderCh, pullFromId)

	// 生产者②：攒批落库 goroutine
	go GoPersist(getDSN(), resultCh, 90)

	// 生产者③：快照 goroutine（每 10 秒写一次）
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			// ★ 注意：这里 book 是指针，快照 goroutine 和撮合 goroutine 共享同一本
			// 但快照只读 FromId / BuySet / SellSet / Cache，撮合器是唯一写者
			// Go 的内存模型保证 channel 传递可见性，但这里我们用更安全的做法：
			//   让撮合 goroutine 自己写快照（tick 时也能拿到正确数据）
			// 所以这个 ticker 改成发送信号给撮合 goroutine
		}
	}()

	// 更简单的做法：让撮合循环自己 tick（单线程写盘口，快照也在这里写）
	snapshotTicker := time.NewTicker(10 * time.Second)
	defer snapshotTicker.Stop()

	fmt.Printf("=== 撮合服务启动（快照恢复 fromId=%d）===\n", savedFromId)
	for {
		select {
		case order := <-orderCh:
			// ★ 撮合器是唯一碰盘口的人（单线程），快照也必须在同一线程写
			// 否则快照 goroutine 和撮合 goroutine 同时碰 OrderBook，锁的开销 + 竞态
			mr := book.match(order)
			book.FromId = order.SeqId // 撮合完更新断点
			for _, r := range mr.Items {
				fmt.Println(itemStr(r))
			}
			resultCh <- mr

		case <-snapshotTicker.C:
			// ★ 快照在撮合线程里写：不用锁，和撮合交替进行，天然正确
			// 撮合完每 10 秒有一个"空闲窗口"，正好写快照
			DumpSnapshot(book, snapshotDir)
		}
	}
}

// 场景模式：第 4 层的回归测试（原 main 的内容原样搬过来）
func runScenarios() {
	data, err := os.ReadFile("scenarios.json")
	if err != nil {
		fmt.Println("读不到 scenarios.json:", err)
		return
	}
	var scenarios []scenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		fmt.Println("scenarios.json 格式错误:", err)
		return
	}

	pass, fail := 0, 0
	for _, sc := range scenarios {
		book := NewOrderBook()
		var got []string
		for i, so := range sc.Orders {
			mr := book.match(buildOrder(so, int64(i)+1))
			for _, r := range mr.Items {
				got = append(got, itemStr(r))
			}
		}

		ok := len(got) == len(sc.Expected)
		if ok {
			for i := range got {
				if got[i] != sc.Expected[i] {
					ok = false
					break
				}
			}
		}

		if ok {
			pass++
			fmt.Printf("PASS  %s\n", sc.Name)
		} else {
			fail++
			fmt.Printf("FAIL  %s\n", sc.Name)
			for i := 0; i < len(got) || i < len(sc.Expected); i++ {
				g, e := "<缺>", "<缺>"
				if i < len(got) {
					g = got[i]
				}
				if i < len(sc.Expected) {
					e = sc.Expected[i]
				}
				mark := "  "
				if g != e {
					mark = "→"
				}
				fmt.Printf("      %s got: %-50s want: %s\n", mark, g, e)
			}
		}
	}
	fmt.Printf("\n%d 通过 / %d 失败\n", pass, fail)
}
