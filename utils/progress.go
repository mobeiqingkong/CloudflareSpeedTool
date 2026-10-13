package utils

import (
	"fmt"
	"io"

	"github.com/cheggaaa/pb/v3"
)

type Bar struct {
	pb *pb.ProgressBar
}

// ProgressOutput 进度条输出目标；为 nil 时使用 pb 的默认输出（stderr）。
// 图形界面版本会将它指向界面日志区，以便用户看到实时测速进度；命令行模式下恒为 nil，行为与原来完全一致。
var ProgressOutput io.Writer

func NewBar(count int, MyStrStart, MyStrEnd string) *Bar {
	tmpl := fmt.Sprintf(`{{counters . }} {{ bar . "[" "-" (cycle . "↖" "↗" "↘" "↙" ) "_" "]"}} %s {{string . "MyStr" | green}} %s `, MyStrStart, MyStrEnd)
	bar := pb.ProgressBarTemplate(tmpl).New(count)
	if ProgressOutput != nil {
		bar.SetWriter(ProgressOutput)
	}
	bar.Start()
	return &Bar{pb: bar}
}

func (b *Bar) Grow(num int, MyStrVal string) {
	b.pb.Set("MyStr", MyStrVal).Add(num)
}

func (b *Bar) Done() {
	b.pb.Finish()
}
