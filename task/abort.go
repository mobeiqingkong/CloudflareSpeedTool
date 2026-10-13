package task

import "sync/atomic"

// 中断机制：图形界面（gui）用它实现「中断」按钮。
//
// 原理：GUI 调用 RequestAbort() 置位标志后，正在运行的延迟测速 / 下载测速
// 会在其循环检查点（见 tcping.go、download.go）尽快退出并返回已测得的部分结果
// （延迟测速约 1 秒内停止，下载测速立即停止）。
//
// 命令行模式下从未调用 RequestAbort()，标志恒为 0，所有检查点均不触发，
// 行为与原来完全一致。
var abortFlag int32

// RequestAbort 请求中断当前正在进行的测速（在下一个检查点生效）
func RequestAbort() {
	atomic.StoreInt32(&abortFlag, 1)
}

// ClearAbort 清除中断标志（开始新一轮测速前调用）
func ClearAbort() {
	atomic.StoreInt32(&abortFlag, 0)
}

// IsAborted 是否已请求中断（测速循环内的检查点使用）
func IsAborted() bool {
	return atomic.LoadInt32(&abortFlag) != 0
}
