package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// defaultPort 默认监听端口（仅绑定 127.0.0.1）
const defaultPort = 7752

func main() {
	// 已有实例在运行？直接把界面窗口调出来，避免重复启动
	if url := probeExisting(); url != "" {
		fmt.Println("检测到程序已在运行，正在打开界面：" + url)
		if err := openAppWindow(url); err != nil {
			fmt.Println("打开界面失败：" + err.Error())
			fmt.Println("请手动在浏览器中访问：" + url)
		}
		return
	}

	app := NewApp()

	// 监听端口（默认端口占用时自动改用随机端口）
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		fmt.Println("启动本地服务失败：", err)
		pauseExit()
		return
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())

	// 控制台提示（在日志重定向之前输出，保证双击运行时能看到）
	fmt.Println("==========================================================")
	fmt.Printf("  CloudflareSpeedTool 图形界面  v%s\n", version)
	fmt.Printf("  界面地址：%s\n", url)
	fmt.Println("  提示：保持本窗口打开；关闭本窗口将退出程序。")
	fmt.Println("==========================================================")

	// 初始化运行环境（把测速日志与进度接到界面）
	app.init()

	// 打开界面窗口
	go func() {
		if err := openAppWindow(url); err != nil {
			app.emitError("打开界面窗口失败：" + err.Error() + "（请手动在浏览器访问 " + url + "）")
		}
	}()

	srv := &http.Server{Handler: app.routes()}
	if err := srv.Serve(ln); err != nil {
		fmt.Println("服务异常退出：", err)
		pauseExit()
	}
}

// probeExisting 探测默认端口上是否已有本程序实例
func probeExisting() string {
	client := http.Client{Timeout: 900 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/status", defaultPort))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var st Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return ""
	}
	if st.Version == "" {
		return "" // 端口上不是本程序
	}
	return fmt.Sprintf("http://127.0.0.1:%d/", defaultPort)
}

// openAppWindow 优先用 Edge / Chrome 的 --app 模式打开独立窗口（无地址栏，观感同桌面应用），
// 找不到时退回系统默认浏览器。
func openAppWindow(url string) error {
	var lastErr error
	for _, exe := range browserCandidates() {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		cmd := exec.Command(exe, "--app="+url, "--window-size=1024,820")
		if err := cmd.Start(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	// 兜底：系统默认浏览器
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		if lastErr != nil {
			return fmt.Errorf("%v；默认浏览器打开也失败：%v", lastErr, err)
		}
		return err
	}
	return nil
}

// browserCandidates 返回常见 Chromium 浏览器的安装路径
func browserCandidates() []string {
	list := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	}
	for _, base := range []string{
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("ProgramFiles"),
		os.Getenv("LOCALAPPDATA"),
	} {
		if base == "" {
			continue
		}
		list = append(list,
			filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(base, `Google\Chrome\Application\chrome.exe`),
		)
	}
	return list
}

// pauseExit Windows 下双击运行时出错，等待用户看清信息
func pauseExit() {
	if runtime.GOOS == "windows" {
		fmt.Println("按回车键退出...")
		fmt.Scanln()
	}
}
