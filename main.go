package main

import (
	"encoding/json"
	"fmt"
	"os"

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

// 服务模式：两条流水线
// ① 拉单器 → chan订单 → 撮合器（单线程碰盘口）
// ② 撮合器 → chan结果 → 攒批器（批量落库，幂等）
func runService() {
	orderCh := make(chan *Order, 5000)   // 传送带①：订单
	resultCh := make(chan MatchResult, 5000) // 传送带②：撮合结果

	// 生产者①：拉单 goroutine（第 5 层）
	go GoPuller(getDSN(), tableName, orderCh, 1)

	// 生产者②：攒批落库 goroutine（第 6 层）
	// 90 条/批 + 1 秒超时；INSERT IGNORE 幂等防重（断点丢了重撮合也不脏表）
	go GoPersist(getDSN(), resultCh, 90)

	// 消费者①：撮合。main goroutine 亲自上场 —— 全进程只有这里碰盘口（单线程）
	book := NewOrderBook()
	fmt.Println("=== 撮合服务启动（已接落库），等待订单 ===")
	for order := range orderCh {
		mr := book.match(order)
		// ① 保留打印（验证用）
		for _, r := range mr.Items {
			fmt.Println(itemStr(r))
		}
		// ② 丢上传送带②，撮合节奏不被数据库拖累
		resultCh <- mr
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
