package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/shopspring/decimal"
)

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

func main() {
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
