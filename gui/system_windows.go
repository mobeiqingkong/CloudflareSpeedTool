//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// uiWindowTitle 界面窗口的标题（与前端页面 <title> 保持一致）
const uiWindowTitle = "Cloudflare 优选 IP 工具"

// watchUIWindow 监视界面窗口是否存在：当全部界面窗口都关闭后回调 onGone（用于自动退出程序）。
// 通过 FindWindowW 按窗口标题查找实现，不依赖页面脚本；宽限期内（等待浏览器启动）不做判定。
func watchUIWindow(onGone func()) {
	user32 := syscall.NewLazyDLL("user32.dll")
	findWindow := user32.NewProc("FindWindowW")

	titlePtr, err := windows.UTF16PtrFromString(uiWindowTitle)
	if err != nil {
		return
	}

	graceUntil := time.Now().Add(45 * time.Second) // 等待浏览器启动并加载页面
	miss := 0
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		h, _, _ := findWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if h != 0 {
			miss = 0
			continue
		}
		if time.Now().Before(graceUntil) {
			continue
		}
		miss++
		if miss >= 2 { // 连续 2 次（约 6 秒）检测不到才判定已关闭，避免偶发误判
			onGone()
			return
		}
	}
}

// showMessageBox 弹出系统消息框（GUI 子系统无控制台时用于向用户提示错误）
func showMessageBox(title, text string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	_, _, _ = messageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x00000010) // MB_ICONERROR
}

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// hiddenCmd 创建不弹出控制台窗口的命令。
// GUI 子系统（-H=windowsgui）程序本身没有控制台，启动控制台子进程（如 ipconfig/netsh）时
// Windows 默认会为其分配一个新的控制台窗口（一闪而过），需用 CREATE_NO_WINDOW 抑制。
func hiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

// IsAdmin 当前进程是否以管理员身份运行
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// clearSystemProxy 清除系统代理设置：
//  1. WinINET（注册表 Internet Settings：代理开关、代理服务器、PAC 自动配置脚本）
//  2. WinHTTP（netsh winhttp reset proxy，需要管理员权限，失败仅提示）
func clearSystemProxy() ([]string, error) {
	var logs []string

	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return logs, fmt.Errorf("打开注册表 Internet Settings 失败：%v", err)
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
		logs = append(logs, "✘ 关闭系统代理开关失败："+err.Error())
	} else {
		logs = append(logs, "✔ 已关闭系统代理开关（ProxyEnable = 0）")
	}

	if v, _, err := k.GetStringValue("ProxyServer"); err == nil && v != "" {
		if err := k.DeleteValue("ProxyServer"); err != nil {
			logs = append(logs, "✘ 清除代理服务器地址失败："+err.Error())
		} else {
			logs = append(logs, "✔ 已清除代理服务器地址（ProxyServer）")
		}
	}

	if v, _, err := k.GetStringValue("AutoConfigURL"); err == nil && v != "" {
		if err := k.DeleteValue("AutoConfigURL"); err != nil {
			logs = append(logs, "✘ 清除自动配置脚本失败："+err.Error())
		} else {
			logs = append(logs, "✔ 已清除代理自动配置脚本（AutoConfigURL / PAC）")
		}
	}

	// 通知系统立即刷新代理设置（否则部分程序不能及时感知注册表变化）
	notifyProxyChanged()

	// WinHTTP 是独立的一套系统级代理设置
	if out, err := hiddenCmd("netsh", "winhttp", "reset", "proxy").CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		logs = append(logs, "! WinHTTP 代理重置跳过（需要管理员权限，可选）："+firstLine(msg))
	} else {
		logs = append(logs, "✔ 已重置 WinHTTP 代理")
	}

	return logs, nil
}

// notifyProxyChanged 通过 InternetSetOption 通知系统代理设置已变更
func notifyProxyChanged() {
	wininet := syscall.NewLazyDLL("wininet.dll")
	proc := wininet.NewProc("InternetSetOptionW")
	_, _, _ = proc.Call(0, 39, 0, 0) // INTERNET_OPTION_SETTINGS_CHANGED = 39
	_, _, _ = proc.Call(0, 37, 0, 0) // INTERNET_OPTION_REFRESH = 37
}
