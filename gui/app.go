package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"

	"github.com/XIU2/CloudflareSpeedTest/task"
	"github.com/XIU2/CloudflareSpeedTest/utils"
)

// version 版本号
var version = "1.0.0"

// ---------- 前后端数据类型 ----------

// LogItem 一条日志
type LogItem struct {
	Time string `json:"time"`
	Msg  string `json:"msg"`
}

// Status 应用状态
type Status struct {
	Version   string `json:"version"`
	Admin     bool   `json:"admin"`
	IPFile    string `json:"ipFile"`
	HasIPFile bool   `json:"hasIpFile"`
	Running   bool   `json:"running"`
	Kind      string `json:"kind"`
}

// TestRequest 测试请求
type TestRequest struct {
	Type       string `json:"type"`       // latency | speed | full
	Domain     string `json:"domain"`     // 要写入 hosts 的域名
	WriteHosts bool   `json:"writeHosts"` // 测试完成后是否写入 hosts
}

// StartResult 启动测试的结果
type StartResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ClearProxyResult 清除系统代理的结果
type ClearProxyResult struct {
	OK    bool     `json:"ok"`
	Logs  []string `json:"logs"`
	Error string   `json:"error,omitempty"`
}

// ---------- App ----------

// App 应用主体
type App struct {
	hub *Hub

	ipFile string // ip.txt 绝对路径
	runDir string // 运行目录（result.csv 与 backup 目录所在）

	runMu       sync.Mutex
	running     bool
	runningKind string
	aborting    bool // 已请求中断（等待当前批次停止）
}

// NewApp 创建应用
func NewApp() *App {
	a := &App{hub: NewHub()}
	a.ipFile, a.runDir = locateFiles()
	return a
}

// locateFiles 定位 ip.txt：依次尝试 exe 所在目录、自身上级目录与当前工作目录
func locateFiles() (ipFile, runDir string) {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		dirs = append(dirs, d)
		for i := 0; i < 5; i++ {
			nd := filepath.Dir(d)
			if nd == d {
				break
			}
			d = nd
			dirs = append(dirs, d)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	for _, d := range dirs {
		p := filepath.Join(d, "ip.txt")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, d
		}
	}
	if len(dirs) > 0 {
		return filepath.Join(dirs[0], "ip.txt"), dirs[0]
	}
	return "ip.txt", "."
}

// init 初始化运行环境（由 main 在服务启动前调用）
func (a *App) init() {
	// 让 task 包使用正确的 ip.txt 与结果文件路径
	task.IPFile = a.ipFile
	utils.Output = filepath.Join(a.runDir, "result.csv")

	// 把 task 包的所有输出（日志、进度条）接到界面上
	a.setupLogging()

	if a.info().HasIPFile {
		a.emitLog("程序就绪，ip.txt：" + a.ipFile)
	} else {
		a.emitError("未找到 ip.txt（IP 段数据文件），请将 ip.txt 放到程序目录后重启应用")
	}
	if !IsAdmin() {
		a.emitLog("! 当前不是管理员权限，写入 hosts 可能失败（可右键以管理员身份运行）")
	}
}

// StartTest 启动一次测速（异步执行，过程与结果通过 SSE 推送）
func (a *App) StartTest(req TestRequest) StartResult {
	switch req.Type {
	case "latency", "speed", "full":
	default:
		return StartResult{Error: "未知的测试类型：" + req.Type}
	}
	if req.WriteHosts {
		d, err := NormalizeDomain(req.Domain)
		if err != nil {
			return StartResult{Error: err.Error()}
		}
		req.Domain = d
	}
	if !a.info().HasIPFile {
		return StartResult{Error: "未找到 ip.txt（IP 段数据文件），无法测速"}
	}

	a.runMu.Lock()
	if a.running {
		a.runMu.Unlock()
		return StartResult{Error: "已有测试正在进行，请等待完成"}
	}
	a.running = true
	a.runningKind = req.Type
	a.runMu.Unlock()

	a.emitStatus()
	go a.runTest(req)
	return StartResult{OK: true}
}

// StopTest 请求中断当前正在进行的测试（异步生效：延迟测速约 1 秒内停止，下载测速立即停止）
func (a *App) StopTest() StartResult {
	a.runMu.Lock()
	if !a.running {
		a.runMu.Unlock()
		return StartResult{Error: "当前没有正在进行的测试"}
	}
	already := a.aborting
	a.aborting = true
	a.runMu.Unlock()

	if !already {
		a.emitLog("⏹ 已请求中断，正在停止测速…")
	}
	task.RequestAbort()
	return StartResult{OK: true}
}

// ClearProxy 清除系统代理
func (a *App) ClearProxy() ClearProxyResult {
	a.emitLog("▶ 开始清除系统代理...")
	logs, err := clearSystemProxy()
	for _, l := range logs {
		a.emitLog(l)
	}
	res := ClearProxyResult{Logs: logs}
	if err != nil {
		res.Error = err.Error()
		a.emitError(err.Error())
		return res
	}
	res.OK = true
	a.emitLog("✔ 清除系统代理完成")
	return res
}

// ---------- 内部：状态与事件 ----------

// info 汇总当前状态
func (a *App) info() Status {
	a.runMu.Lock()
	running, kind := a.running, a.runningKind
	a.runMu.Unlock()

	hasIP := false
	if st, err := os.Stat(a.ipFile); err == nil && !st.IsDir() {
		hasIP = true
	}
	return Status{
		Version:   version,
		Admin:     IsAdmin(),
		IPFile:    a.ipFile,
		HasIPFile: hasIP,
		Running:   running,
		Kind:      kind,
	}
}

// emit 向前端推送事件
func (a *App) emit(event string, payload interface{}) {
	a.hub.Emit(event, payload)
}

// emitLog 记录并推送一条日志（历史由 Hub 缓冲，页面刷新后回放）
func (a *App) emitLog(msg string) {
	a.emit("log", LogItem{Time: time.Now().Format("15:04:05"), Msg: msg})
}

// emitError 记录一条错误
func (a *App) emitError(msg string) {
	a.emitLog("✘ " + msg)
	a.emit("err", map[string]string{"msg": msg})
}

// emitStage 推送阶段变化（latency / download）
func (a *App) emitStage(stage, status string, count int) {
	a.emit("stage", map[string]interface{}{
		"stage":  stage,
		"status": status,
		"count":  count,
	})
}

// emitStatus 推送状态变化
func (a *App) emitStatus() {
	a.emit("status", a.info())
}

// ---------- 内部：日志重定向 ----------

// setupLogging 把父项目库（utils/task）的输出接到界面日志区：
//  1. utils 的颜色输出（color.Output）
//  2. 进度条输出（utils.ProgressOutput，pb 默认写 stderr 无法被界面读取）
//  3. os.Stdout / os.Stderr 兜底（防止直接打印的输出漏网）
func (a *App) setupLogging() {
	w := &eventWriter{app: a}
	color.Output = w
	// pb 进度条在非终端环境输出的帧之间没有任何分隔符（连 \r 都没有），
	// 用 frameFramer 在每次 Write（= 一帧）后补一个 \r，恢复帧边界供 drain 切分
	utils.ProgressOutput = &frameFramer{w: w}

	if r, pw, err := os.Pipe(); err == nil {
		os.Stdout = pw
		os.Stderr = pw
		go func() {
			// 与 eventWriter 相同的解析逻辑：\r 快照归入进度、\n 行归入日志
			w2 := &eventWriter{app: a}
			buf := make([]byte, 4096)
			for {
				n, err := r.Read(buf)
				if n > 0 {
					_, _ = w2.Write(buf[:n])
				}
				if err != nil {
					return
				}
			}
		}()
	}
}

var (
	// ansiRe 匹配终端控制序列（颜色、擦除行、光标控制等）
	ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]|\x1b\\][^\x07]*\x07")
	// ansiSepRe 匹配"清行 / 光标归位"序列：进度条以它们刷新帧（而非 \r），需视为帧分隔符
	ansiSepRe = regexp.MustCompile("\x1b\\[[0-9;]*[GK]")
)

func cleanANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// frameFramer 包装一个 io.Writer：每次 Write 后追加一个 \r 作为帧分隔符
// （pb 在非终端环境下连续输出帧且不带任何分隔符，用它恢复帧边界）
type frameFramer struct {
	w io.Writer
}

func (f *frameFramer) Write(p []byte) (int, error) {
	if _, err := f.w.Write(p); err != nil {
		return 0, err
	}
	_, _ = f.w.Write([]byte("\r"))
	return len(p), nil
}

// eventWriter 把写入内容按行/进度分段转发给界面
// 以 \r 结尾的片段视为进度快照（替换式刷新），以 \n 结尾的视为日志行（追加式）
type eventWriter struct {
	app *App
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *eventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	w.drain()
	return len(p), nil
}

func (w *eventWriter) drain() {
	for {
		b := w.buf.Bytes()

		// 找最早的分隔点：\r / \n（日志行、回车刷新）或 ANSI 清行/归位序列（进度帧刷新）
		crlfPos := bytes.IndexAny(b, "\r\n")
		sepPos, sepLen := -1, 0
		if m := ansiSepRe.FindIndex(b); m != nil {
			sepPos, sepLen = m[0], m[1]-m[0]
		}
		if crlfPos < 0 && sepPos < 0 {
			return
		}

		var seg string
		var newline bool
		if crlfPos >= 0 && (sepPos < 0 || crlfPos <= sepPos) {
			seg = string(b[:crlfPos])
			if b[crlfPos] == '\r' && crlfPos+1 < len(b) && b[crlfPos+1] == '\n' {
				newline = true
				w.buf.Next(crlfPos + 2)
			} else {
				newline = b[crlfPos] == '\n'
				w.buf.Next(crlfPos + 1)
			}
		} else {
			// ANSI 分隔：帧前内容视为进度快照
			seg = string(b[:sepPos])
			w.buf.Next(sepPos + sepLen)
		}

		seg = cleanANSI(seg)
		seg = strings.TrimRight(seg, " ")
		if strings.TrimSpace(seg) == "" {
			continue
		}
		if len(seg) > 500 {
			seg = seg[:500]
		}
		if newline && len(seg) <= 200 {
			w.app.emitLog(seg)
		} else {
			w.app.emit("progress", map[string]string{"text": seg})
		}
	}
}
