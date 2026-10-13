package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	hostsMarkerStart = "# ======== CloudflareSpeedTool START ========"
	hostsMarkerEnd   = "# ======== CloudflareSpeedTool END ========"
)

// HostsInfo hosts 写入结果
type HostsInfo struct {
	Written   bool     `json:"written"`
	IP        string   `json:"ip"`
	Domain    string   `json:"domain"`
	Path      string   `json:"path"`
	Backup    string   `json:"backup"`
	NeedAdmin bool     `json:"needAdmin"`
	Error     string   `json:"error,omitempty"`
	Logs      []string `json:"-"`
}

// NormalizeDomain 从用户输入中提取纯域名（去掉协议、路径、端口），并校验
func NormalizeDomain(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("请先填写要加速的域名，例如 github.com")
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:] // 去掉协议头
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i] // 去掉路径 / 查询串
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		if _, err := strconv.Atoi(s[i+1:]); err == nil {
			s = s[:i] // 去掉端口号
		}
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if err := validateDomain(s); err != nil {
		return "", err
	}
	return s, nil
}

func validateDomain(s string) error {
	if len(s) == 0 || len(s) > 253 {
		return fmt.Errorf("域名长度不合法：%s", s)
	}
	if !strings.Contains(s, ".") {
		return fmt.Errorf("域名格式不正确：%s（应形如 github.com）", s)
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_'
		if !ok {
			return fmt.Errorf("域名包含不支持的字符：%s", s)
		}
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("域名格式不正确：%s", s)
		}
	}
	return nil
}

// WriteHosts 把「IP 域名」写入系统 hosts：
//  1. 先把原 hosts 备份到 runDir/backup/
//  2. 移除本工具上次写入的标记区块
//  3. 把 hosts 中原有的同域名记录注释掉（保留原文，可随时恢复）
//  4. 写入新的标记区块，并刷新 DNS 缓存
func WriteHosts(ip, domain, runDir string) *HostsInfo {
	info := &HostsInfo{IP: ip, Domain: domain}
	path := hostsPath()
	info.Path = path

	raw, err := os.ReadFile(path)
	if err != nil {
		info.Error = fmt.Sprintf("读取 hosts 失败：%v", err)
		info.Logs = append(info.Logs, "✘ "+info.Error)
		return info
	}

	// 1. 备份
	if backup, err := backupHosts(raw, runDir); err == nil {
		info.Backup = backup
		info.Logs = append(info.Logs, "已备份原 hosts："+backup)
	} else {
		info.Logs = append(info.Logs, "! hosts 备份失败："+err.Error())
	}

	// 2. 生成新内容：移除旧标记区块、注释同域名旧记录、追加新标记区块
	result, tookOver := buildHostsContent(string(raw), ip, domain)
	if tookOver > 0 {
		info.Logs = append(info.Logs, fmt.Sprintf("已注释 hosts 中原有的 %d 条同域名记录（前缀 [CFST 已接管]）", tookOver))
	}

	// 4. 写回（保持原文件权限）
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode()
	}
	if err := os.WriteFile(path, []byte(result), mode); err != nil {
		if os.IsPermission(err) {
			info.NeedAdmin = true
			info.Error = "没有写入权限，请右键以管理员身份运行程序后重试"
		} else {
			info.Error = err.Error()
		}
		info.Logs = append(info.Logs, "✘ 写入 hosts 失败："+info.Error)
		return info
	}

	// 5. 刷新 DNS 缓存
	if out, err := hiddenCmd("ipconfig", "/flushdns").CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		info.Logs = append(info.Logs, "! 刷新 DNS 缓存失败："+firstLine(msg))
	} else {
		info.Logs = append(info.Logs, "已刷新 DNS 缓存（ipconfig /flushdns）")
	}

	info.Written = true
	info.Logs = append(info.Logs, fmt.Sprintf("✔ 已写入 hosts：%s %s", ip, domain))
	return info
}

// buildHostsContent 生成新的 hosts 内容（纯函数，便于单元测试）：
//  1. 移除本工具上次写入的标记区块
//  2. 把原有的同域名记录注释掉（前缀 [CFST 已接管]，保留原文可随时恢复）
//  3. 追加新的标记区块
//
// 返回新内容与被接管的旧记录数
func buildHostsContent(raw, ip, domain string) (string, int) {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+4)
	inBlock := false
	tookOver := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == hostsMarkerStart {
			inBlock = true
			continue
		}
		if trimmed == hostsMarkerEnd {
			inBlock = false
			continue
		}
		if inBlock {
			continue // 丢弃旧标记区块内容
		}
		matched := false
		for _, h := range hostLineHostnames(trimmed) {
			if strings.EqualFold(h, domain) {
				matched = true
				break
			}
		}
		if matched {
			out = append(out, "# [CFST 已接管] "+trimmed)
			tookOver++
		} else {
			out = append(out, line)
		}
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	out = append(out, "", hostsMarkerStart, ip+" "+domain, hostsMarkerEnd, "")
	return strings.Join(out, "\r\n"), tookOver
}

// hostsPath 系统 hosts 文件路径
func hostsPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "drivers", "etc", "hosts")
}

// backupHosts 备份 hosts 到 runDir/backup/
func backupHosts(raw []byte, runDir string) (string, error) {
	dir := filepath.Join(runDir, "backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "hosts_"+time.Now().Format("20060102_150405")+".bak")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// hostLineHostnames 解析一条 hosts 记录中的主机名（跳过空行与注释行）
func hostLineHostnames(line string) []string {
	l := strings.TrimSpace(line)
	if l == "" || strings.HasPrefix(l, "#") {
		return nil
	}
	if i := strings.IndexByte(l, '#'); i >= 0 {
		l = l[:i] // 去掉行内注释
	}
	fields := strings.Fields(l)
	if len(fields) < 2 {
		return nil
	}
	return fields[1:]
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}
