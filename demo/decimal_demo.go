package main

import (
	"fmt"

	"github.com/shopspring/decimal"
)

func main() {
	// ===== 现场 1：float 算错账 =====
	a := 0.1
	b := 0.2
	fmt.Println("float64:  0.1 + 0.2 =", a+b) // 不是 0.3！

	// ===== 现场 2：decimal 算对账 =====
	// NewFromString 返回两个值：结果 + 错误。用 _ 表示"这个错误我先不管"（demo 里可以这样，正式代码要处理）
	x, _ := decimal.NewFromString("0.1")
	y, _ := decimal.NewFromString("0.2")
	fmt.Println("decimal:  0.1 + 0.2 =", x.Add(y))

	// ===== decimal 的三件套 =====
	// 1) 比较：不能写 > < ==，要用方法
	p, _ := decimal.NewFromString("50000.5")
	q, _ := decimal.NewFromString("49999.9")
	fmt.Println("p > q ?", p.GreaterThan(q))

	// 2) 乘法
	fmt.Println("0.3 × 3 =", decimal.RequireFromString("0.3").Mul(decimal.NewFromInt(3)))

	// 3) 除法：还记得"100 USDT 买 7 USDT 的币能买几个"吗
	fmt.Println("100 ÷ 7 =", decimal.NewFromInt(100).Div(decimal.NewFromInt(7)))
}
