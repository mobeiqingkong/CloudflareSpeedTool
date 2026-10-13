package main

import (
	"strings"
	"testing"
)

// TestNormalizeDomain 域名归一化
func TestNormalizeDomain(t *testing.T) {
	okCases := map[string]string{
		"github.com":                       "github.com",
		"  github.com  ":                   "github.com",
		"https://github.com":               "github.com",
		"http://github.com/user/repo?x=1":  "github.com",
		"HTTP://GitHub.COM:443/path":       "github.com",
		"sub.domain.co.uk":                 "sub.domain.co.uk",
		"https://www.example.com/a/b#hash": "www.example.com",
	}
	for in, want := range okCases {
		got, err := NormalizeDomain(in)
		if err != nil {
			t.Errorf("NormalizeDomain(%q) 报错: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeDomain(%q) = %q，期望 %q", in, got, want)
		}
	}

	badCases := []string{
		"",
		"   ",
		"invalid",       // 无点
		"has space.com", // 含空格
		"中文.com",        // 非 ASCII
		"-bad.com",      // 以 - 开头
		"bad-.com",      // 以 - 结尾
		".com",          // 以点开头
		"a..b",          // 连续点（首段空）
	}
	for _, in := range badCases {
		if _, err := NormalizeDomain(in); err == nil {
			t.Errorf("NormalizeDomain(%q) 应该报错但通过了", in)
		}
	}
}

// TestHostLineHostnames hosts 行解析
func TestHostLineHostnames(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"# comment", 0},
		{"1.2.3.4", 0},
		{"1.2.3.4 a.com", 1},
		{"1.2.3.4 a.com b.com", 2},
		{"1.2.3.4\ta.com", 1},
		{"1.2.3.4 a.com # inline comment", 1},
		{"# 1.2.3.4 commented.com", 0},
	}
	for _, c := range cases {
		got := hostLineHostnames(c.line)
		if len(got) != c.want {
			t.Errorf("hostLineHostnames(%q) = %v，期望 %d 个", c.line, got, c.want)
		}
	}
}

// TestBuildHostsContentEmpty 空 hosts 首次写入
func TestBuildHostsContentEmpty(t *testing.T) {
	out, took := buildHostsContent("", "1.2.3.4", "github.com")
	if took != 0 {
		t.Errorf("空 hosts 不应有接管记录，got %d", took)
	}
	if !strings.Contains(out, hostsMarkerStart) || !strings.Contains(out, hostsMarkerEnd) {
		t.Error("缺少标记区块")
	}
	if !strings.Contains(out, "1.2.3.4 github.com") {
		t.Error("缺少 IP 域名记录")
	}
	if !strings.Contains(out, "\r\n") {
		t.Error("输出应使用 CRLF 换行")
	}
}

// TestBuildHostsContentReplaceBlock 重复写入时旧标记区块被替换、不重复
func TestBuildHostsContentReplaceBlock(t *testing.T) {
	first, _ := buildHostsContent("", "1.1.1.1", "github.com")
	second, took := buildHostsContent(first, "2.2.2.2", "github.com")

	if took != 0 {
		t.Errorf("旧标记区块内容不应算接管，got %d", took)
	}
	if strings.Contains(second, "1.1.1.1 github.com") {
		t.Error("旧标记区块中的记录应被移除")
	}
	if !strings.Contains(second, "2.2.2.2 github.com") {
		t.Error("缺少新的记录")
	}
	if strings.Count(second, hostsMarkerStart) != 1 {
		t.Errorf("标记区块应只有一个，got %d", strings.Count(second, hostsMarkerStart))
	}
}

// TestBuildHostsContentTakeOver 同域名旧记录被注释
func TestBuildHostsContentTakeOver(t *testing.T) {
	raw := "# sample\r\n127.0.0.1 localhost\r\n5.5.5.5 GitHub.COM\r\n6.6.6.6 other.com github.com\r\n7.7.7.7 unrelated.com\r\n"
	out, took := buildHostsContent(raw, "1.2.3.4", "github.com")

	if took != 2 {
		t.Errorf("应接管 2 条记录，got %d", took)
	}
	if !strings.Contains(out, "# [CFST 已接管] 5.5.5.5 GitHub.COM") {
		t.Error("大小写不同的同域名记录应被注释")
	}
	if !strings.Contains(out, "# [CFST 已接管] 6.6.6.6 other.com github.com") {
		t.Error("多主机名行应整行注释")
	}
	if !strings.Contains(out, "127.0.0.1 localhost") || strings.Contains(out, "# [CFST 已接管] 127.0.0.1") {
		t.Error("无关记录不应被动")
	}
	if !strings.Contains(out, "7.7.7.7 unrelated.com") {
		t.Error("无关记录应保留原样")
	}
	if !strings.Contains(out, "1.2.3.4 github.com") {
		t.Error("缺少新记录")
	}
}

// TestBuildHostsContentIdempotent 连续两次写入结果稳定
func TestBuildHostsContentIdempotent(t *testing.T) {
	raw := "127.0.0.1 localhost\r\n"
	first, _ := buildHostsContent(raw, "1.1.1.1", "a.com")
	second, took := buildHostsContent(first, "1.1.1.1", "a.com")
	if took != 0 {
		t.Errorf("幂等写入不应有接管，got %d", took)
	}
	if first != second {
		t.Errorf("幂等写入结果不一致：\n第一次:\n%q\n第二次:\n%q", first, second)
	}
}

// TestBuildHostsContentCRLFPreserve 原文件 CRLF 换行保持
func TestBuildHostsContentCRLFPreserve(t *testing.T) {
	raw := "# a\r\n127.0.0.1 localhost\r\n"
	out, _ := buildHostsContent(raw, "1.1.1.1", "x.com")
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Error("输出中不应存在裸 LF 换行")
	}
}

// TestValidDomainEdge 边界域名
func TestValidDomainEdge(t *testing.T) {
	if err := validateDomain("a.b"); err != nil {
		t.Errorf("a.b 应合法: %v", err)
	}
	if err := validateDomain(strings.Repeat("a", 254)); err == nil {
		t.Error("超长域名应报错")
	}
	if err := validateDomain("_dmarc.example.com"); err != nil {
		t.Errorf("下划线域名应合法: %v", err)
	}
}
