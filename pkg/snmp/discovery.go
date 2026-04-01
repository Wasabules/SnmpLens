package snmp

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// DiscoveryResult represents the outcome of scanning a single IP.
type DiscoveryResult struct {
	IP           string `json:"ip"`
	SysName      string `json:"sysName"`
	SysDescr     string `json:"sysDescr"`
	SysUpTime    string `json:"sysUpTime"`
	ResponseTime int64  `json:"responseTime"`
	Reachable    bool   `json:"reachable"`
	Error        string `json:"error,omitempty"`
}

// Discover scans a CIDR range for SNMP-responsive devices.
func (c *Client) Discover(cidr, community, version string, port, timeoutSec int, v3 V3Params) []DiscoveryResult {
	ips, err := expandCIDR(cidr)
	if err != nil {
		return []DiscoveryResult{{IP: cidr, Error: err.Error()}}
	}

	var wg sync.WaitGroup
	resultsChan := make(chan DiscoveryResult, len(ips))
	sem := make(chan struct{}, 50)
	total := len(ips)

	for i, ip := range ips {
		wg.Add(1)
		go func(ipAddr string, index int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			result := DiscoveryResult{IP: ipAddr}

			g, err := c.newGoSNMP(ipAddr, community, version, port, timeoutSec, 0, v3)
			if err != nil {
				result.Error = err.Error()
				result.ResponseTime = time.Since(start).Milliseconds()
				resultsChan <- result
				runtime.EventsEmit(c.ctx, "discoveryProgress", map[string]interface{}{"current": index + 1, "total": total, "ip": ipAddr})
				return
			}
			if err = g.Connect(); err != nil {
				result.Error = fmt.Sprintf("connect failed: %v", err)
				result.ResponseTime = time.Since(start).Milliseconds()
				resultsChan <- result
				runtime.EventsEmit(c.ctx, "discoveryProgress", map[string]interface{}{"current": index + 1, "total": total, "ip": ipAddr})
				return
			}
			defer g.Conn.Close()

			oids := []string{
				".1.3.6.1.2.1.1.1.0", // sysDescr
				".1.3.6.1.2.1.1.5.0", // sysName
				".1.3.6.1.2.1.1.3.0", // sysUpTime
			}
			packet, err := g.Get(oids)
			result.ResponseTime = time.Since(start).Milliseconds()

			if err != nil {
				result.Error = fmt.Sprintf("get failed: %v", err)
				resultsChan <- result
				runtime.EventsEmit(c.ctx, "discoveryProgress", map[string]interface{}{"current": index + 1, "total": total, "ip": ipAddr})
				return
			}

			result.Reachable = true
			for _, v := range packet.Variables {
				val := formatSnmpValue(v)
				switch v.Name {
				case ".1.3.6.1.2.1.1.1.0":
					result.SysDescr = fmt.Sprintf("%v", val)
				case ".1.3.6.1.2.1.1.5.0":
					result.SysName = fmt.Sprintf("%v", val)
				case ".1.3.6.1.2.1.1.3.0":
					result.SysUpTime = fmt.Sprintf("%v", val)
				}
			}
			resultsChan <- result
			runtime.EventsEmit(c.ctx, "discoveryProgress", map[string]interface{}{"current": index + 1, "total": total, "ip": ipAddr})
		}(ip, i)
	}

	wg.Wait()
	close(resultsChan)

	results := make([]DiscoveryResult, 0, len(ips))
	for r := range resultsChan {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool {
		return compareIPs(results[i].IP, results[j].IP)
	})
	return results
}

func expandCIDR(cidr string) ([]string, error) {
	if net.ParseIP(cidr) != nil {
		return []string{cidr}, nil
	}

	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR or IP: %s", cidr)
	}

	var ips []string
	for ip := ip.Mask(ipNet.Mask); ipNet.Contains(ip); incrementIP(ip) {
		ips = append(ips, ip.String())
	}
	if len(ips) > 2 {
		ips = ips[1 : len(ips)-1]
	}
	return ips, nil
}

func incrementIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func compareIPs(a, b string) bool {
	ipA := net.ParseIP(a)
	ipB := net.ParseIP(b)
	if ipA == nil || ipB == nil {
		return a < b
	}
	return string(ipA.To16()) < string(ipB.To16())
}
