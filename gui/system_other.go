//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// IsAdmin 非 Windows 平台检查是否为 root
func IsAdmin() bool {
	return os.Geteuid() == 0
}

// hiddenCmd 非 Windows 平台无需处理控制台窗口（直接创建命令）
func hiddenCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// clearSystemProxy 非 Windows 平台暂不支持清除系统代理
func clearSystemProxy() ([]string, error) {
	return nil, fmt.Errorf("当前平台暂不支持清除系统代理（该功能仅支持 Windows）")
}

// watchUIWindow 非 Windows 平台暂不支持界面窗口检测（不会自动退出）
func watchUIWindow(onGone func()) {}

// showMessageBox 非 Windows 平台输出到控制台
func showMessageBox(title, text string) {
	fmt.Printf("[%s] %s\n", title, text)
}
