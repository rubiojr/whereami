//go:build vecmap_rhi

package main

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// processCosts is what the viewer process consumed, measured the same way for
// every renderer: CPU time and peak RSS from getrusage, GPU engine time and
// current GPU memory from the kernel's DRM fdinfo for this process's clients.
// The rest splits those totals: CPU by thread name, and resident memory
// between the Go runtime and native code (Qt, drivers).
type processCosts struct {
	user, system time.Duration
	maxRSSKiB    int64
	engines      map[string]time.Duration
	vramKiB      uint64
	gttKiB       uint64
	threads      []threadCPU
	exited       time.Duration // threads that ended before the costs were read
	goRuntime    goRuntime
	status       map[string]uint64 // /proc/self/status sizes in KiB
	peak         memorySample      // the sampled moment of highest RSS
	live         memorySample      // the sampled moment of the largest live heap
}

// readProcessCosts must run before the window's graphics context is destroyed,
// which closes its DRM clients and their counters.
func readProcessCosts() (processCosts, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return processCosts{}, err
	}
	costs := processCosts{user: time.Duration(usage.Utime.Nano()), system: time.Duration(usage.Stime.Nano()),
		maxRSSKiB: usage.Maxrss, engines: map[string]time.Duration{}, goRuntime: readGoRuntime()}
	threads, err := readThreadCPU("/proc/self/task", os.Getpid())
	if err != nil {
		return costs, err
	}
	costs.threads = threads
	costs.exited = costs.user + costs.system
	for _, thread := range threads {
		costs.exited -= thread.cpu
	}
	costs.exited = max(0, costs.exited) // schedstat and getrusage account separately
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return costs, err
	}
	costs.status = parseStatusKiB(string(status))
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

// threadCPU is the on-CPU time of the live threads that share a name.
type threadCPU struct {
	name    string
	threads int
	cpu     time.Duration
}

// readThreadCPU groups the schedstat CPU time of the threads under root
// (/proc/self/task) by name, busiest first. The thread whose ID is pid is
// "main", Qt's GUI thread here. Go's own threads carry the process name.
func readThreadCPU(root string, pid int) ([]threadCPU, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	groups := map[string]*threadCPU{}
	for _, entry := range entries {
		comm, commErr := os.ReadFile(filepath.Join(root, entry.Name(), "comm"))
		stat, statErr := os.ReadFile(filepath.Join(root, entry.Name(), "schedstat"))
		fields := strings.Fields(string(stat))
		if commErr != nil || statErr != nil || len(fields) == 0 {
			continue // threads exit while scanning
		}
		ns, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		name := strings.Join(strings.Fields(string(comm)), "_")
		if entry.Name() == strconv.Itoa(pid) {
			name = "main"
		}
		group := groups[name]
		if group == nil {
			group = &threadCPU{name: name}
			groups[name] = group
		}
		group.threads++
		group.cpu += time.Duration(ns)
	}
	threads := make([]threadCPU, 0, len(groups))
	for _, group := range groups {
		threads = append(threads, *group)
	}
	slices.SortFunc(threads, func(a, b threadCPU) int {
		return cmp.Or(cmp.Compare(b.cpu, a.cpu), strings.Compare(a.name, b.name))
	})
	return threads, nil
}

// parseStatusKiB reads the kB-sized fields of /proc/self/status.
func parseStatusKiB(text string) map[string]uint64 {
	sizes := map[string]uint64{}
	for line := range strings.Lines(text) {
		key, value, ok := strings.Cut(line, ":")
		fields := strings.Fields(value)
		if !ok || len(fields) != 2 || fields[1] != "kB" {
			continue
		}
		if size, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			sizes[key] = size
		}
	}
	return sizes
}

// goRuntime is the Go runtime's share of the costs. gcCPU is the runtime's
// own estimate; residentBytes is what it has mapped and not released, and
// liveBytes the heap the last collection found reachable.
type goRuntime struct {
	gcCPU                                     time.Duration
	gcCycles, allocBytes                      uint64
	residentBytes, heapObjectBytes, liveBytes uint64
}

var goRuntimeMetrics = []string{"/cpu/classes/gc/total:cpu-seconds", "/gc/cycles/total:gc-cycles", "/gc/heap/allocs:bytes",
	"/memory/classes/total:bytes", "/memory/classes/heap/released:bytes", "/memory/classes/heap/objects:bytes", "/gc/heap/live:bytes"}

func readGoRuntime() goRuntime {
	samples := make([]metrics.Sample, len(goRuntimeMetrics))
	for i, name := range goRuntimeMetrics {
		samples[i].Name = name
	}
	metrics.Read(samples)
	value := func(i int) uint64 {
		if samples[i].Value.Kind() != metrics.KindUint64 {
			return 0
		}
		return samples[i].Value.Uint64()
	}
	var gcCPU time.Duration
	if samples[0].Value.Kind() == metrics.KindFloat64 {
		gcCPU = time.Duration(samples[0].Value.Float64() * float64(time.Second))
	}
	return goRuntime{gcCPU: gcCPU, gcCycles: value(1), allocBytes: value(2),
		residentBytes: value(3) - value(4), heapObjectBytes: value(5), liveBytes: value(6)}
}

// memorySample is resident memory at one moment, split between the Go
// runtime and everything else. at is when, from the start of the measurement.
type memorySample struct {
	rssKiB, goKiB, heapObjectKiB, liveKiB uint64
	at                                    time.Duration
}

func readMemorySample() (memorySample, bool) {
	statm, err := os.ReadFile("/proc/self/statm")
	fields := strings.Fields(string(statm))
	if err != nil || len(fields) < 2 {
		return memorySample{}, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return memorySample{}, false
	}
	g := readGoRuntime()
	return memorySample{rssKiB: pages * uint64(os.Getpagesize()) >> 10, goKiB: g.residentBytes >> 10, heapObjectKiB: g.heapObjectBytes >> 10,
		liveKiB: g.liveBytes >> 10}, true
}

// measurement spans one run: it samples memory to split peak RSS and writes
// optional Go profiles, and stops where the process costs are read.
type measurement struct {
	cpuProfile *os.File
	heapPath   string
	stop, done chan struct{}
	started    time.Time
	// Owned by sample until done closes: the samples of highest RSS and of
	// the largest live heap, and the live heap peakPath's profile was written at.
	peak, live memorySample
	peakPath   string
	written    uint64
	finished   bool
	err        error
}

// startMeasurement starts sampling memory and, with cpuPath, CPU profiling.
// heapPath receives a heap profile when the costs are read, and peakPath one
// each time the live heap grows 2% past the last one written, so it ends at
// the largest live heap of the run.
func startMeasurement(cpuPath, heapPath, peakPath string) (*measurement, error) {
	m := &measurement{heapPath: heapPath, peakPath: peakPath, stop: make(chan struct{}), done: make(chan struct{}), started: time.Now()}
	if cpuPath != "" {
		file, err := os.Create(cpuPath)
		if err != nil {
			return nil, err
		}
		if err := pprof.StartCPUProfile(file); err != nil {
			file.Close()
			return nil, err
		}
		m.cpuProfile = file
	}
	go m.sample()
	return m, nil
}

func (m *measurement) sample() {
	defer close(m.done)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if sample, ok := readMemorySample(); ok {
			sample.at = time.Since(m.started)
			if sample.rssKiB > m.peak.rssKiB {
				m.peak = sample
			}
			if sample.liveKiB > m.live.liveKiB {
				m.live = sample
				if m.peakPath != "" && sample.liveKiB > m.written+m.written/50 {
					m.err = errors.Join(m.err, writeHeapProfile(m.peakPath))
					m.written = sample.liveKiB
				}
			}
		}
		select {
		case <-m.stop:
			return
		case <-ticker.C:
		}
	}
}

// finish reads the process costs, then stops sampling and profiling. It must
// run before the window's graphics context is destroyed (readProcessCosts).
// Without a measurement it only reads the costs.
func (m *measurement) finish() (processCosts, error) {
	costs, err := readProcessCosts()
	if m == nil || m.finished {
		return costs, err
	}
	m.finished = true
	close(m.stop)
	<-m.done
	costs.peak, costs.live = m.peak, m.live
	if m.cpuProfile != nil {
		pprof.StopCPUProfile()
		m.err = m.cpuProfile.Close()
	}
	if m.heapPath != "" {
		runtime.GC() // the heap profile reports the last completed cycle
		m.err = errors.Join(m.err, writeHeapProfile(m.heapPath))
	}
	return costs, err
}

// close finishes a run that ended before reading its costs and reports
// profile write errors.
func (m *measurement) close() error {
	if !m.finished {
		m.finish()
	}
	return m.err
}

func writeHeapProfile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	return errors.Join(pprof.Lookup("heap").WriteTo(file, 0), file.Close())
}

func (c processCosts) report() {
	fmt.Printf("process cpu_user=%s cpu_system=%s max_rss_kib=%d\n", c.user, c.system, c.maxRSSKiB)
	engines := make([]string, 0, len(c.engines))
	for _, engine := range slices.Sorted(maps.Keys(c.engines)) {
		engines = append(engines, fmt.Sprintf("gpu_%s=%s", engine, c.engines[engine]))
	}
	fmt.Printf("drm %s gpu_vram_kib=%d gpu_gtt_kib=%d\n", strings.Join(engines, " "), c.vramKiB, c.gttKiB)
	threads := make([]string, 0, len(c.threads)+1)
	for _, thread := range c.threads {
		name := thread.name
		if thread.threads > 1 {
			name += fmt.Sprintf(":%d", thread.threads)
		}
		threads = append(threads, fmt.Sprintf("%s=%s", name, thread.cpu.Round(time.Millisecond)))
	}
	threads = append(threads, fmt.Sprintf("exited=%s", c.exited.Round(time.Millisecond)))
	fmt.Printf("threads %s\n", strings.Join(threads, " "))
	g := c.goRuntime
	fmt.Printf("go gc_cpu=%s gc_cycles=%d alloc_bytes=%d resident_kib=%d heap_object_kib=%d\n", g.gcCPU.Round(time.Millisecond), g.gcCycles, g.allocBytes, g.residentBytes>>10, g.heapObjectBytes>>10)
	fmt.Printf("memory rss_kib=%d rss_anon_kib=%d rss_file_kib=%d rss_shmem_kib=%d peak_sample_rss_kib=%d peak_sample_go_kib=%d peak_sample_go_heap_object_kib=%d peak_sample_at=%v peak_live_kib=%d peak_live_at=%v\n",
		c.status["VmRSS"], c.status["RssAnon"], c.status["RssFile"], c.status["RssShmem"], c.peak.rssKiB, c.peak.goKiB, c.peak.heapObjectKiB, c.peak.at.Round(time.Millisecond),
		c.live.liveKiB, c.live.at.Round(time.Millisecond))
}
