package main

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const snapshotExt = ".snapshot"

// 快照载荷：不直接 gob OrderBook（treeset 内部字段私有编不了），
// 而是把两棵树拍扁成两个 Order 数组，存 FromId + 方向
type SnapshotPayload struct {
	FromId  int64
	BuySide []Order  // 买盘所有挂单（直接存 Order 值，gob 能编）
	SellSide []Order // 卖盘所有挂单
}

// ===== 写快照 =====
func DumpSnapshot(book *OrderBook, dir string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Println("快照目录创建失败:", err)
		return
	}

	// ① 拍扁：把两棵树里的 Order 对象抓出来，Copy 成值放进数组
	payload := SnapshotPayload{FromId: book.FromId}
	for _, v := range book.BuySet.Values() {
		o := v.(*Order)
		payload.BuySide = append(payload.BuySide, *o) // deref：值拷贝
	}
	for _, v := range book.SellSet.Values() {
		o := v.(*Order)
		payload.SellSide = append(payload.SellSide, *o)
	}

	pathName := filepath.Join(dir, fmt.Sprintf("%020d%s", book.FromId, snapshotExt))
	tmpName := filepath.Join(dir, "TMP@snapshot@TMP")

	// ② 删残留临时文件
	if _, err := os.Stat(tmpName); err == nil {
		os.Remove(tmpName)
	}

	// ③ 写临时文件
	file, err := os.Create(tmpName)
	if err != nil {
		fmt.Println("快照写临时文件失败:", err)
		return
	}
	if err := gob.NewEncoder(file).Encode(payload); err != nil {
		file.Close()
		fmt.Println("快照 gob 编码失败:", err)
		return
	}
	file.Close()

	// ④ rename：原子操作
	if err := os.Rename(tmpName, pathName); err != nil {
		fmt.Println("快照 rename 失败:", err)
		return
	}
	fmt.Printf("  📸 快照 fromId=%d 已落盘 (买%d 卖%d)\n", book.FromId, len(payload.BuySide), len(payload.SellSide))
}

// ===== 找最新快照 =====
func FindLatestSnapshot(dir string) (path string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var ids []int64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, snapshotExt) {
			continue
		}
		idStr := strings.TrimSuffix(name, snapshotExt)
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	return filepath.Join(dir, fmt.Sprintf("%020d%s", ids[0], snapshotExt))
}

// ===== 从快照恢复 =====
func LoadSnapshot(dir string) (*OrderBook, int64) {
	path := FindLatestSnapshot(dir)
	if path == "" {
		return NewOrderBook(), 0
	}

	file, err := os.Open(path)
	if err != nil {
		fmt.Println("快照打开失败:", err)
		return NewOrderBook(), 0
	}
	defer file.Close()

	var payload SnapshotPayload
	if err := gob.NewDecoder(file).Decode(&payload); err != nil {
		fmt.Println("快照 gob 解码失败:", err)
		return NewOrderBook(), 0
	}

	// 用 Enqueue 重建盘口（值拷贝变回指针）
	book := NewOrderBook()
	book.FromId = payload.FromId
	for i := range payload.BuySide {
		o := payload.BuySide[i] // deref：值拷贝
		book.Enqueue(&o)
	}
	for i := range payload.SellSide {
		o := payload.SellSide[i]
		o.BuyOrSell = Sell
		book.Enqueue(&o)
	}

	fmt.Printf("  🔄 从快照恢复 fromId=%d，挂单 买%d 卖%d\n", payload.FromId, len(payload.BuySide), len(payload.SellSide))
	return book, payload.FromId
}
