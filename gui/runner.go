package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/task"
	"github.com/XIU2/CloudflareSpeedTest/utils"
)

// maxRows 推送给界面的最大结果行数
const maxRows = 50

// ResultRow 一行测速结果
type ResultRow struct {
	IP    string  `json:"ip"`
	Delay float64 `json:"delay"` // 平均延迟（毫秒）
	Loss  float64 `json:"loss"`  // 丢包率 0~1
	Speed float64 `json:"speed"` // 下载速度（MB/s）
	Colo  string  `json:"colo"`  // 地区码
	Score float64 `json:"score"` // 综合得分（仅综合测试）
}

// TestResult 测试完成后的完整结果
type TestResult struct {
	TestType  string      `json:"testType"`
	Rows      []ResultRow `json:"rows"`
	Best      *ResultRow  `json:"best"`
	Hosts     *HostsInfo  `json:"hosts,omitempty"`
	Total     int         `json:"total"`     // 延迟测速得到可用 IP 数
	ElapsedMS int64       `json:"elapsedMs"` // 总用时（毫秒）
}

func testLabel(kind string) string {
	switch kind {
	case "latency":
		return "延迟测试"
	case "speed":
		return "速度测试"
	case "full":
		return "综合测试"
	}
	return kind
}

// runTest 在后台执行一次测试（由 StartTest 启动），测试串行执行
func (a *App) runTest(req TestRequest) {
	defer func() {
		if r := recover(); r != nil {
			a.emitError(fmt.Sprintf("测试异常：%v", r))
		}
		a.runMu.Lock()
		a.running = false
		a.runningKind = ""
		a.runMu.Unlock()
		a.emitStatus()
	}()

	startAt := time.Now()
	extra := ""
	if req.WriteHosts && req.Domain != "" {
		extra = fmt.Sprintf("，完成后写入 hosts：%s", req.Domain)
	}
	a.emitLog("▶ 开始" + testLabel(req.Type) + extra)

	// ---------- 阶段 1：延迟测速 ----------
	a.emitStage("latency", "start", 0)
	pingSet := task.NewPing().Run().FilterDelay().FilterLossRate()
	a.emitStage("latency", "done", len(pingSet))

	if len(pingSet) == 0 {
		a.emitError("延迟测速没有找到可用 IP：请检查 ip.txt 是否存在、网络是否正常、是否开启了代理")
		return
	}
	a.emitLog(fmt.Sprintf("延迟测速完成：可用 IP %d 个", len(pingSet)))

	switch req.Type {
	case "latency":
		utils.ExportCsv([]utils.CloudflareIPData(pingSet))
		rows := pingRows(pingSet, maxRows)
		a.finishTest(req, rows, len(pingSet), startAt)

	case "speed", "full":
		// ---------- 阶段 2：下载测速 ----------
		a.emitStage("download", "start", 0)
		speedSet := task.TestDownloadSpeed(pingSet)
		a.emitStage("download", "done", len(speedSet))
		if len(speedSet) == 0 {
			a.emitError("下载测速没有结果，请稍后重试")
			return
		}
		utils.ExportCsv([]utils.CloudflareIPData(speedSet))
		rows := speedRows(speedSet, maxRows)
		if req.Type == "full" {
			rows = applyScoreAndSort(rows)
			a.emitLog("综合测试：按 0.7×速度得分 + 0.3×延迟得分 排序，选出最均衡的 IP")
		}
		a.finishTest(req, rows, len(pingSet), startAt)
	}
}

// finishTest 汇总结果、按需写入 hosts、推送前端
func (a *App) finishTest(req TestRequest, rows []ResultRow, total int, startAt time.Time) {
	if len(rows) == 0 {
		a.emitError("没有可用结果")
		return
	}

	best := rows[0]
	res := TestResult{
		TestType:  req.Type,
		Rows:      rows,
		Best:      &best,
		Total:     total,
		ElapsedMS: time.Since(startAt).Milliseconds(),
	}

	a.emitLog(fmt.Sprintf("★ 最优 IP：%s（延迟 %.1f ms，丢包 %.0f%%%s%s）",
		best.IP, best.Delay, best.Loss*100, speedText(best.Speed), coloText(best.Colo)))

	// 写入 hosts
	if req.WriteHosts && req.Domain != "" {
		info := WriteHosts(best.IP, req.Domain, a.runDir)
		res.Hosts = info
		for _, l := range info.Logs {
			a.emitLog(l)
		}
	}

	a.emit("result", res)
	a.emitLog(fmt.Sprintf("✔ %s完成，用时 %.1f 秒", testLabel(req.Type), time.Since(startAt).Seconds()))
}

// ---------- 数据转换 ----------

func pingRows(set utils.PingDelaySet, n int) []ResultRow {
	rows := make([]ResultRow, 0, n)
	for i, v := range set {
		if i >= n {
			break
		}
		rows = append(rows, ResultRow{
			IP:    v.IP.String(),
			Delay: float64(v.Delay.Microseconds()) / 1000,
			Loss:  lossRate(v.Sended, v.Received),
			Colo:  v.Colo,
		})
	}
	return rows
}

func speedRows(set utils.DownloadSpeedSet, n int) []ResultRow {
	rows := make([]ResultRow, 0, n)
	for i, v := range set {
		if i >= n {
			break
		}
		rows = append(rows, ResultRow{
			IP:    v.IP.String(),
			Delay: float64(v.Delay.Microseconds()) / 1000,
			Loss:  lossRate(v.Sended, v.Received),
			Speed: v.DownloadSpeed / 1024 / 1024,
			Colo:  v.Colo,
		})
	}
	return rows
}

func lossRate(sended, received int) float64 {
	if sended <= 0 {
		return 0
	}
	return float64(sended-received) / float64(sended)
}

// applyScoreAndSort 综合评分：0.7 × 速度归一化 + 0.3 × 延迟归一化，按得分降序
func applyScoreAndSort(rows []ResultRow) []ResultRow {
	if len(rows) == 0 {
		return rows
	}
	maxSpeed := 0.0
	minDelay := math.MaxFloat64
	for _, r := range rows {
		if r.Speed > maxSpeed {
			maxSpeed = r.Speed
		}
		if r.Delay > 0 && r.Delay < minDelay {
			minDelay = r.Delay
		}
	}
	for i := range rows {
		speedNorm := 0.0
		if maxSpeed > 0 {
			speedNorm = rows[i].Speed / maxSpeed
		}
		delayNorm := 0.0
		if rows[i].Delay > 0 && minDelay < math.MaxFloat64 {
			delayNorm = minDelay / rows[i].Delay
		}
		rows[i].Score = 0.7*speedNorm + 0.3*delayNorm
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Score > rows[j].Score })
	return rows
}

// ---------- 小工具 ----------

func speedText(v float64) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("，速度 %.2f MB/s", v)
}

func coloText(colo string) string {
	if colo == "" {
		return ""
	}
	return "，地区 " + colo
}
