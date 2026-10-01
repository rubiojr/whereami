//go:build vecmap_rhi

package main

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// processCosts is what the viewer process consumed, measured the same way for
// every renderer: CPU time and peak RSS from getrusage, GPU engine time and
// current GPU memory from the kernel's DRM fdinfo for this process's clients.
type processCosts struct {
	user, system time.Duration
	maxRSSKiB    int64
	engines      map[string]time.Duration
	vramKiB      uint64
	gttKiB       uint64
}

// readProcessCosts must run before the window's graphics context is destroyed,
// which closes its DRM clients and their counters.
func readProcessCosts() (processCosts, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return processCosts{}, err
	}
	costs := processCosts{user: time.Duration(usage.Utime.Nano()), system: time.Duration(usage.Stime.Nano()),
		maxRSSKiB: usage.Maxrss, engines: map[string]time.Duration{}}
	paths, err := filepath.Glob("/proc/self/fdinfo/*")
	if err != nil {
		return costs, err
	}
	seen := map[string]bool{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // descriptors close while scanning
		}
		client, ok := parseDRMClient(string(data))
		if !ok || seen[client.id] {
			continue // duplicated descriptors share one client
		}
		seen[client.id] = true
		for engine, busy := range client.engines {
			costs.engines[engine] += busy
		}
		costs.vramKiB += client.vramKiB
		costs.gttKiB += client.gttKiB
	}
	return costs, nil
}

type drmClient struct {
	id              string
	engines         map[string]time.Duration
	vramKiB, gttKiB uint64
}

// parseDRMClient reads the DRM usage stats of one fdinfo entry. Entries for
// other descriptors have no drm-client-id.
func parseDRMClient(text string) (drmClient, bool) {
	client := drmClient{engines: map[string]time.Duration{}}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		switch {
		case key == "drm-client-id":
			client.id = fields[0]
		case strings.HasPrefix(key, "drm-engine-") && len(fields) == 2 && fields[1] == "ns":
			if busy, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
				client.engines[strings.TrimPrefix(key, "drm-engine-")] = time.Duration(busy)
			}
		case (key == "drm-total-vram" || key == "drm-total-gtt") && len(fields) == 2 && fields[1] == "KiB":
			if size, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
				if key == "drm-total-vram" {
					client.vramKiB = size
				} else {
					client.gttKiB = size
				}
			}
		}
	}
	return client, client.id != ""
}

func (c processCosts) report() {
	fmt.Printf("process cpu_user=%s cpu_system=%s max_rss_kib=%d\n", c.user, c.system, c.maxRSSKiB)
	engines := make([]string, 0, len(c.engines))
	for _, engine := range slices.Sorted(maps.Keys(c.engines)) {
		engines = append(engines, fmt.Sprintf("gpu_%s=%s", engine, c.engines[engine]))
	}
	fmt.Printf("drm %s gpu_vram_kib=%d gpu_gtt_kib=%d\n", strings.Join(engines, " "), c.vramKiB, c.gttKiB)
}
