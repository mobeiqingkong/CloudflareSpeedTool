package task

import (
	"sync"
	"testing"
)

func TestAbortFlag(t *testing.T) {
	ClearAbort()
	if IsAborted() {
		t.Fatal("初始状态不应为「已中断」")
	}
	RequestAbort()
	if !IsAborted() {
		t.Fatal("RequestAbort 之后应处于「已中断」")
	}
	ClearAbort()
	if IsAborted() {
		t.Fatal("ClearAbort 之后应恢复为「未中断」")
	}
}

func TestAbortFlagConcurrent(t *testing.T) {
	ClearAbort()
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RequestAbort()
			_ = IsAborted()
			ClearAbort()
		}()
	}
	wg.Wait()
	// 并发读写不 panic / 不数据竞争（go test -race 下验证）即为通过
	if IsAborted() {
		ClearAbort()
	}
}
