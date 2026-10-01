package main

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// wp_presentation_feedback kind flags.
const (
	kindVsync = 1 << iota
	kindHWClock
	kindHWCompletion
	kindZeroCopy
)

// feedback is the compositor's answer for one surface commit. Times are
// nanoseconds on the presentation clock; the preload stamps commits with it.
type feedback struct {
	surface       uint32
	commit, shown uint64
	refresh       uint32
	seq           uint64
	flags         uint32
	discarded     bool
}

type presentationLog struct {
	clock     uint32
	supported bool
	feedback  []feedback
}

func parseLog(data []byte) (presentationLog, error) {
	log := presentationLog{supported: true}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		values, err := parseFields(fields)
		if err != nil {
			return log, fmt.Errorf("line %d: %w", line, err)
		}
		switch {
		case fields[0] == "wayland-present" && len(values) == 1 && values[0] == 1:
		case fields[0] == "clock" && len(values) == 1:
			log.clock = uint32(values[0])
		case fields[0] == "unsupported" && len(values) == 0:
			log.supported = false
		case fields[0] == "presented" && len(values) == 6:
			log.feedback = append(log.feedback, feedback{surface: uint32(values[0]), commit: values[1], shown: values[2], refresh: uint32(values[3]), seq: values[4], flags: uint32(values[5])})
		case fields[0] == "discarded" && len(values) == 2:
			log.feedback = append(log.feedback, feedback{surface: uint32(values[0]), commit: values[1], discarded: true})
		default:
			return log, fmt.Errorf("line %d: unexpected record %q", line, scanner.Text())
		}
	}
	return log, scanner.Err()
}

func parseFields(fields []string) ([]uint64, error) {
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

type summary struct {
	clock, surface       uint32
	surfaces             int
	presented, discarded int
	refresh              time.Duration
	intervals, latencies []time.Duration
	late, missed         int
	vsync, hwClock       int
	hwCompletion, zero   int
}

// summarize reports the surface with the most feedback, which is the one the
// client draws into; cursor and decoration surfaces commit rarely. The first
// warmup presented frames are not sampled.
func summarize(log presentationLog, warmup int) (summary, error) {
	counts := map[uint32]int{}
	for _, f := range log.feedback {
		counts[f.surface]++
	}
	if len(counts) == 0 {
		return summary{}, errors.New("no presentation feedback; did the client commit a frame?")
	}
	s := summary{clock: log.clock, surfaces: len(counts)}
	for surface, count := range counts {
		if count > counts[s.surface] || count == counts[s.surface] && surface < s.surface {
			s.surface = surface
		}
	}
	var shown []feedback
	for _, f := range log.feedback {
		switch {
		case f.surface != s.surface:
		case f.discarded:
			s.discarded++
		default:
			shown = append(shown, f)
		}
	}
	s.presented = len(shown)
	slices.SortFunc(shown, func(a, b feedback) int { return cmp.Compare(a.shown, b.shown) })
	shown = shown[min(warmup, len(shown)):]
	var refreshes []time.Duration
	for i, f := range shown {
		s.count(f.flags)
		if f.shown >= f.commit {
			s.latencies = append(s.latencies, time.Duration(f.shown-f.commit))
		}
		if f.refresh > 0 {
			refreshes = append(refreshes, time.Duration(f.refresh))
		}
		if i == 0 {
			continue
		}
		s.intervals = append(s.intervals, time.Duration(f.shown-shown[i-1].shown))
		if periods := refreshPeriods(shown[i-1], f); periods > 1 {
			s.late++
			s.missed += periods - 1
		}
	}
	if len(refreshes) > 0 {
		slices.Sort(refreshes)
		s.refresh = refreshes[len(refreshes)/2]
	}
	return s, nil
}

func (s *summary) count(flags uint32) {
	for _, kind := range []struct {
		flag  uint32
		count *int
	}{{kindVsync, &s.vsync}, {kindHWClock, &s.hwClock}, {kindHWCompletion, &s.hwCompletion}, {kindZeroCopy, &s.zero}} {
		if flags&kind.flag != 0 {
			*kind.count++
		}
	}
}

// refreshPeriods counts display refreshes between two presentations: from the
// vblank sequence of vsynced frames, else from the refresh duration. Zero means
// the compositor reported neither.
func refreshPeriods(previous, next feedback) int {
	if previous.flags&next.flags&kindVsync != 0 && previous.seq != 0 && next.seq > previous.seq {
		return int(next.seq - previous.seq)
	}
	if previous.refresh > 0 {
		return int(math.Round(float64(next.shown-previous.shown) / float64(previous.refresh)))
	}
	return 0
}

func (s summary) print(w io.Writer) {
	fmt.Fprintf(w, "presentation surface=%d surfaces=%d presented=%d discarded=%d clock=%d refresh=%s\n", s.surface, s.surfaces, s.presented, s.discarded, s.clock, s.refresh)
	for _, metric := range []struct {
		name   string
		values []time.Duration
	}{{"presentation_interval", s.intervals}, {"presentation_latency", s.latencies}} {
		if len(metric.values) == 0 {
			continue
		}
		values := slices.Clone(metric.values)
		slices.Sort(values)
		fmt.Fprintf(w, "%s samples=%d p50=%s p95=%s p99=%s max=%s\n", metric.name, len(values), values[len(values)/2], values[min(len(values)-1, len(values)*95/100)], values[min(len(values)-1, len(values)*99/100)], values[len(values)-1])
	}
	fmt.Fprintf(w, "presentation_refreshes late_frames=%d missed_refreshes=%d\n", s.late, s.missed)
	fmt.Fprintf(w, "presentation_flags vsync=%d hw_clock=%d hw_completion=%d zero_copy=%d\n", s.vsync, s.hwClock, s.hwCompletion, s.zero)
}
