//go:build !windows

package main

import (
	"fmt"
	"os"
)

// IsAdmin 非 Windows 平台检查是否为 root
func IsAdmin() bool {
	return os.Geteuid() == 0
}

// clearSystemProxy 非 Windows 平台暂不支持清除系统代理
func clearSystemProxy() ([]string, error) {
	return nil, fmt.Errorf("当前平台暂不支持清除系统代理（该功能仅支持 Windows）")
}
