//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

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
	if out, err := exec.Command("netsh", "winhttp", "reset", "proxy").CombinedOutput(); err != nil {
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
