package network

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// PingResult holds ping statistics from pro-bing.
type PingResult struct {
	Target      string    `json:"target"`
	Sent        int       `json:"sent"`
	Received    int       `json:"received"`
	LossPercent float64   `json:"lossPercent"`
	MinMs       float64   `json:"minMs"`
	AvgMs       float64   `json:"avgMs"`
	MaxMs       float64   `json:"maxMs"`
	Replies     []float64 `json:"replies"`
}

// TracerouteHop represents a single hop in a traceroute.
type TracerouteHop struct {
	Hop     int    `json:"hop"`
	IP      string `json:"ip"`
	RTT1    string `json:"rtt1"`
	RTT2    string `json:"rtt2"`
	RTT3    string `json:"rtt3"`
	Timeout bool   `json:"timeout"`
}

// Ping uses pro-bing for cross-platform ICMP ping without elevated privileges.
func Ping(target string, count int) (PingResult, error) {
	if count <= 0 {
		count = 4
	}
	result := PingResult{Target: target}

	pinger, err := probing.NewPinger(target)
	if err != nil {
		return result, fmt.Errorf("resolve target: %w", err)
	}

	pinger.Count = count
	pinger.Timeout = time.Duration(count)*time.Second + 5*time.Second

	// On Windows, use privileged mode (uses Windows ICMP API internally)
	if runtime.GOOS == "windows" {
		pinger.SetPrivileged(true)
	}

	// Collect individual RTTs
	pinger.OnRecv = func(pkt *probing.Packet) {
		result.Replies = append(result.Replies, float64(pkt.Rtt.Microseconds())/1000.0)
	}

	if err := pinger.Run(); err != nil {
		return result, fmt.Errorf("ping failed: %w", err)
	}

	stats := pinger.Statistics()
	result.Sent = stats.PacketsSent
	result.Received = stats.PacketsRecv
	result.LossPercent = stats.PacketLoss
	result.MinMs = float64(stats.MinRtt.Microseconds()) / 1000.0
	result.AvgMs = float64(stats.AvgRtt.Microseconds()) / 1000.0
	result.MaxMs = float64(stats.MaxRtt.Microseconds()) / 1000.0

	return result, nil
}

// Traceroute uses system traceroute/tracert command with OS-specific arguments.
// Emits "tracerouteProgress" events per hop via Wails runtime.
func Traceroute(ctx context.Context, target string) ([]TracerouteHop, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "tracert", "-d", "-w", "2000", target)
	} else {
		// Linux / macOS
		cmd = exec.CommandContext(ctx, "traceroute", "-n", "-w", "2", target)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("traceroute pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("traceroute start: %w", err)
	}

	var hops []TracerouteHop
	scanner := bufio.NewScanner(stdout)

	if runtime.GOOS == "windows" {
		hops = parseWindowsTraceroute(ctx, scanner)
	} else {
		hops = parseUnixTraceroute(ctx, scanner)
	}

	cmd.Wait()
	return hops, nil
}

// parseWindowsTraceroute parses Windows tracert output.
// Format: "  1     1 ms     1 ms     1 ms  192.168.1.1"
func parseWindowsTraceroute(ctx context.Context, scanner *bufio.Scanner) []TracerouteHop {
	hopRe := regexp.MustCompile(`^\s*(\d+)\s+(.+)$`)
	rttRe := regexp.MustCompile(`(\d+)\s*ms|\*`)
	ipRe := regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)

	var hops []TracerouteHop
	for scanner.Scan() {
		line := scanner.Text()
		m := hopRe.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}

		hopNum, _ := strconv.Atoi(m[1])
		rest := m[2]

		// Extract RTTs
		rttMatches := rttRe.FindAllString(rest, 3)
		rtts := formatRTTs(rttMatches)

		// Extract IP (last IP-like pattern on the line)
		ipMatch := ipRe.FindAllString(rest, -1)
		ip := ""
		if len(ipMatch) > 0 {
			ip = ipMatch[len(ipMatch)-1]
		}

		hop := TracerouteHop{
			Hop:     hopNum,
			IP:      ip,
			RTT1:    rtts[0],
			RTT2:    rtts[1],
			RTT3:    rtts[2],
			Timeout: rtts[0] == "*" && rtts[1] == "*" && rtts[2] == "*",
		}
		hops = append(hops, hop)
		wailsRuntime.EventsEmit(ctx, "tracerouteProgress", hop)
	}
	return hops
}

// parseUnixTraceroute parses Linux/macOS traceroute output.
// Format: " 1  192.168.1.1  1.234 ms  0.987 ms  1.123 ms"
func parseUnixTraceroute(ctx context.Context, scanner *bufio.Scanner) []TracerouteHop {
	hopRe := regexp.MustCompile(`^\s*(\d+)\s+(.+)$`)
	rttRe := regexp.MustCompile(`([\d.]+)\s*ms|\*`)
	ipRe := regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)

	var hops []TracerouteHop
	for scanner.Scan() {
		line := scanner.Text()
		m := hopRe.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}

		hopNum, _ := strconv.Atoi(m[1])
		rest := m[2]

		// Extract IP
		ipMatch := ipRe.FindString(rest)
		ip := ipMatch

		// Extract RTTs
		rttMatches := rttRe.FindAllStringSubmatch(rest, 3)
		rtts := []string{"*", "*", "*"}
		for i, rm := range rttMatches {
			if i >= 3 {
				break
			}
			if len(rm) > 1 && rm[1] != "" {
				rtts[i] = rm[1] + " ms"
			}
		}

		hop := TracerouteHop{
			Hop:     hopNum,
			IP:      ip,
			RTT1:    rtts[0],
			RTT2:    rtts[1],
			RTT3:    rtts[2],
			Timeout: rtts[0] == "*" && rtts[1] == "*" && rtts[2] == "*",
		}
		hops = append(hops, hop)
		wailsRuntime.EventsEmit(ctx, "tracerouteProgress", hop)
	}
	return hops
}

func formatRTTs(matches []string) [3]string {
	rtts := [3]string{"*", "*", "*"}
	for i, m := range matches {
		if i >= 3 {
			break
		}
		m = strings.TrimSpace(m)
		if m == "*" {
			continue
		}
		// Already contains "ms", keep as-is
		if strings.Contains(m, "ms") {
			rtts[i] = m
		} else {
			rtts[i] = m + " ms"
		}
	}
	return rtts
}
