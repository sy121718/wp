package utils

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// PortProcess 表示占用端口的进程信息。
type PortProcess struct {
	PID  int
	Name string
}

// KillCommandByPID 返回当前系统下终止指定 PID 的命令示例。
func KillCommandByPID(pid int) string {
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf("taskkill /PID %d /F", pid)
	default:
		return fmt.Sprintf("kill -9 %d", pid)
	}
}

// FormatProcesses 将进程列表格式化为可读字符串。
func FormatProcesses(processes []PortProcess) string {
	return formatProcesses(processes)
}

// IsTCPPortInUse 检查 TCP 端口是否被占用。
func IsTCPPortInUse(port int) (bool, error) {
	if err := validatePort(port); err != nil {
		return false, err
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		if isAddrInUseErr(err) {
			return true, nil
		}
		return false, err
	}

	_ = ln.Close()
	return false, nil
}

// ListTCPListeningProcesses 查询指定 TCP 端口上的监听进程。
func ListTCPListeningProcesses(port int) ([]PortProcess, error) {
	if err := validatePort(port); err != nil {
		return nil, err
	}

	switch runtime.GOOS {
	case "windows":
		return listTCPListeningProcessesWindows(port)
	default:
		return listTCPListeningProcessesUnix(port)
	}
}

// ReleaseTCPPortIfOccupied 释放已被占用的 TCP 端口。
//
// allowedProcessNames 为空表示允许结束任意占用进程；
// 非空时仅结束白名单进程，其他进程会阻止释放并返回错误。
func ReleaseTCPPortIfOccupied(port int, allowedProcessNames []string) error {
	processes, err := ListTCPListeningProcesses(port)
	if err != nil {
		return err
	}
	if len(processes) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(allowedProcessNames))
	for _, name := range allowedProcessNames {
		trimmed := strings.ToLower(strings.TrimSpace(name))
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}

	selfPID := os.Getpid()
	blocked := make([]PortProcess, 0)
	for _, proc := range processes {
		if proc.PID == selfPID {
			continue
		}

		if len(allowed) > 0 {
			if _, ok := allowed[strings.ToLower(proc.Name)]; !ok {
				blocked = append(blocked, proc)
				continue
			}
		}

		if err := killProcessByPID(proc.PID); err != nil {
			return fmt.Errorf("结束进程失败 pid=%d name=%s: %w", proc.PID, proc.Name, err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	inUse, err := IsTCPPortInUse(port)
	if err != nil {
		return err
	}
	if !inUse {
		return nil
	}

	if len(blocked) > 0 {
		return fmt.Errorf("端口 %d 仍被占用，非白名单进程: %s", port, formatProcesses(blocked))
	}
	return fmt.Errorf("端口 %d 释放失败，请手动检查占用进程", port)
}

// EnsurePortReady handles port occupancy strategy by server mode.
//
// debug/test: auto-release only allowlisted processes on the target port.
// others: only report occupancy and provide manual kill commands.
func EnsurePortReady(serverMode string, port int) error {
	return EnsurePortReadyWithStrategy("", serverMode, port)
}

// EnsurePortReadyWithStrategy handles port occupancy strategy by explicit config or server mode fallback.
func EnsurePortReadyWithStrategy(strategy string, serverMode string, port int) error {
	strategy = resolvePortStrategy(strategy, serverMode)
	mode := strings.ToLower(strings.TrimSpace(serverMode))
	allowKill := []string{"main.exe", "main", "go.exe", "go", "air.exe", "air"}

	switch strategy {
	case "auto_release":
		if err := ReleaseTCPPortIfOccupied(port, allowKill); err != nil {
			return fmt.Errorf("启动前端口检查失败: %w", err)
		}
		return nil
	default:
		processes, err := ListTCPListeningProcesses(port)
		if err != nil {
			return fmt.Errorf("启动前端口检查失败: %w", err)
		}
		if len(processes) == 0 {
			return nil
		}

		commands := make([]string, 0, len(processes))
		for _, proc := range processes {
			commands = append(commands, fmt.Sprintf("%s(pid=%d): %s", proc.Name, proc.PID, KillCommandByPID(proc.PID)))
		}

		return fmt.Errorf(
			"端口 %d 已被占用（%s 模式不会自动结束进程）: %s；可手动执行：%s",
			port,
			mode,
			FormatProcesses(processes),
			strings.Join(commands, " ; "),
		)
	}
}

func resolvePortStrategy(strategy string, serverMode string) string {
	normalized := strings.ToLower(strings.TrimSpace(strategy))
	switch normalized {
	case "auto_release", "report_only":
		return normalized
	}

	mode := strings.ToLower(strings.TrimSpace(serverMode))
	switch mode {
	case "debug", "test":
		return "auto_release"
	default:
		return "report_only"
	}
}

func validatePort(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("非法端口: %d", port)
	}
	return nil
}

func isAddrInUseErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address")
}

func listTCPListeningProcessesWindows(port int) ([]PortProcess, error) {
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("执行 netstat 失败: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	target := ":" + strconv.Itoa(port)
	seen := make(map[int]struct{})
	result := make([]PortProcess, 0)

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		if !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		if !strings.HasSuffix(fields[1], target) {
			continue
		}

		state := strings.ToUpper(fields[3])
		if state != "LISTENING" && state != "侦听" {
			continue
		}

		pid, err := strconv.Atoi(fields[4])
		if err != nil {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}

		name, _ := getProcessNameWindows(pid)
		if name == "" {
			name = "unknown"
		}
		result = append(result, PortProcess{PID: pid, Name: name})
	}

	return result, nil
}

func getProcessNameWindows(pid int) (string, error) {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(string(out))
	if content == "" || strings.HasPrefix(strings.ToUpper(content), "INFO:") {
		return "", nil
	}

	reader := csv.NewReader(strings.NewReader(content))
	record, err := reader.Read()
	if err != nil || len(record) == 0 {
		return "", err
	}
	return strings.TrimSpace(record[0]), nil
}

func listTCPListeningProcessesUnix(port int) ([]PortProcess, error) {
	cmd := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-t")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(strings.TrimSpace(string(out))) == 0 {
			return []PortProcess{}, nil
		}
		return nil, fmt.Errorf("执行 lsof 失败: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	seen := make(map[int]struct{})
	result := make([]PortProcess, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		pid, convErr := strconv.Atoi(text)
		if convErr != nil {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}

		name, _ := getProcessNameUnix(pid)
		if name == "" {
			name = "unknown"
		}
		result = append(result, PortProcess{PID: pid, Name: name})
	}
	return result, nil
}

func getProcessNameUnix(pid int) (string, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func killProcessByPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("非法 pid: %d", pid)
	}
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F")
		return cmd.Run()
	default:
		cmd := exec.Command("kill", "-9", strconv.Itoa(pid))
		return cmd.Run()
	}
}

func formatProcesses(processes []PortProcess) string {
	if len(processes) == 0 {
		return ""
	}

	items := make([]string, 0, len(processes))
	for _, proc := range processes {
		items = append(items, fmt.Sprintf("%s(pid=%d)", proc.Name, proc.PID))
	}
	slices.Sort(items)
	return strings.Join(items, ", ")
}
