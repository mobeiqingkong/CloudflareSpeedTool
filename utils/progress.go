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
		// 固定进度条宽度（GUI 模式专用）：避免 pb 去探测终端宽度。
		// pb 在探测失败或识别为 msys/mingw 环境（MSYSTEM/MINGW_PREFIX 环境变量）时，
		// 会回退执行外部 `tput cols` 命令获取宽度（每次渲染一次）；
		// 而无控制台的 GUI 程序启动控制台子进程会弹出新终端窗口，导致反复弹窗。
		bar.SetWidth(100)
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
