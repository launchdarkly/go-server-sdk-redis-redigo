package ldredis

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	r "github.com/gomodule/redigo/redis"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
)

// Contention benchmarks comparing the two upsert modes. Both run in the same process against the
// same server, so the comparison is not exposed to drift between separate runs.
//
// Each simulated SDK instance gets its own store and its own connection pool, because in production
// each instance is a separate process. Sharing one 16-connection pool across all writers would add
// queueing that no real deployment has.
//
// The reported metrics matter more than ns/op here:
//
//	err%          share of updates that ran out of attempts and wrote nothing. The SDK treats these
//	              as store-unavailable, so any non-zero value is a correctness problem.
//	superseded%   share refused by the version check because a newer version was already stored.
//	p50/p99/p999  per-call latency percentiles in microseconds. The pathology is tail-shaped -- a
//	              minority of calls burning every attempt -- and a mean hides that.
//	cmds/op       server-side command executions per update, from INFO commandstats. Includes retries
//	              and the commands the script issues.
//
// ns/op is wall-clock per operation across all writers, which is inverse throughput.
//
// The modes differ in round trips per attempt: the script does 2 (HGET, EVALSHA) and WATCH does 3
// (WATCH, HGET, then MULTI, HSET, and EXEC pipelined into one flush). On loopback a round trip is
// tens of microseconds and syscall-bound, which understates that difference, so the rtt= benchmarks
// insert a delay proxy to make the numbers reflect a real network.

// ---------------------------------------------------------------------------------------------
// delay proxy -- injects a fixed round-trip time so results reflect a real network
// ---------------------------------------------------------------------------------------------

// delayProxy forwards TCP to Redis, sleeping half the target round-trip time in each direction.
// Because it delays each TCP segment rather than each command, a pipelined batch costs one round trip
// and a separate call costs its own, which is the real behavior.
type delayProxy struct {
	ln   net.Listener
	half time.Duration
}

func startDelayProxy(tb testing.TB, target string, rtt time.Duration) *delayProxy {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("proxy listen: %v", err)
	}
	p := &delayProxy{ln: ln, half: rtt / 2}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.handle(c, target)
		}
	}()
	return p
}

func (p *delayProxy) handle(client net.Conn, target string) {
	up, err := net.Dial("tcp", target)
	if err != nil {
		_ = client.Close()
		return
	}
	go p.pipe(client, up)
	p.pipe(up, client)
}

func (p *delayProxy) pipe(dst, src net.Conn) {
	defer dst.Close() //nolint:errcheck // benchmark cleanup
	defer src.Close() //nolint:errcheck // benchmark cleanup
	buf := make([]byte, 128<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if p.half > 0 {
				time.Sleep(p.half)
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *delayProxy) url() string { return "redis://" + p.ln.Addr().String() }
func (p *delayProxy) stop()       { _ = p.ln.Close() }

// ---------------------------------------------------------------------------------------------
// harness
// ---------------------------------------------------------------------------------------------

type tally struct {
	errs       atomic.Int64
	superseded atomic.Int64
	written    atomic.Int64
	durs       [][]time.Duration // one slice per worker; each worker owns its index
}

func (t *tally) record(w int, d time.Duration, updated bool, err error) {
	t.durs[w] = append(t.durs[w], d)
	switch {
	case err != nil:
		t.errs.Add(1)
	case !updated:
		t.superseded.Add(1)
	default:
		t.written.Add(1)
	}
}

func (t *tally) report(b *testing.B, cmds float64) {
	n := float64(b.N)
	b.ReportMetric(float64(t.errs.Load())/n*100, "err%")
	b.ReportMetric(float64(t.superseded.Load())/n*100, "superseded%")
	b.ReportMetric(cmds/n, "cmds/op")

	var all []time.Duration
	for _, s := range t.durs {
		all = append(all, s...)
	}
	if len(all) == 0 {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	q := func(p float64) float64 {
		i := int(p * float64(len(all)-1))
		return float64(all[i].Nanoseconds()) / 1000 // microseconds
	}
	b.ReportMetric(q(0.50), "p50us")
	b.ReportMetric(q(0.99), "p99us")
	b.ReportMetric(q(0.999), "p999us")
}

func resetStats(tb testing.TB, url string) {
	tb.Helper()
	c, err := r.DialURL(url)
	if err != nil {
		tb.Fatalf("dial: %v", err)
	}
	defer c.Close() //nolint:errcheck // benchmark cleanup
	if _, err := c.Do("CONFIG", "RESETSTAT"); err != nil {
		tb.Fatalf("resetstat: %v", err)
	}
}

func totalCommands(tb testing.TB, url string) float64 {
	tb.Helper()
	c, err := r.DialURL(url)
	if err != nil {
		tb.Fatalf("dial: %v", err)
	}
	defer c.Close() //nolint:errcheck // benchmark cleanup
	info, err := r.String(c.Do("INFO", "commandstats"))
	if err != nil {
		tb.Fatalf("info: %v", err)
	}
	var total float64
	for _, line := range strings.Split(info, "\n") {
		if !strings.HasPrefix(line, "cmdstat_") {
			continue
		}
		for _, f := range strings.Split(line[strings.Index(line, ":")+1:], ",") {
			if strings.HasPrefix(f, "calls=") {
				if v, err := strconv.ParseFloat(strings.TrimSpace(f[6:]), 64); err == nil {
					total += v
				}
			}
		}
	}
	return total
}

func seedItems(tb testing.TB, store *redisDataStoreImpl, count, valueSize int) {
	tb.Helper()
	kind := ldstoreimpl.Features()
	c := store.getConn()
	defer c.Close() //nolint:errcheck // benchmark cleanup
	for i := range count {
		key := fmt.Sprintf("flag-%d", i)
		if err := c.Send("HSET", store.featuresKey(kind), key,
			serializedFlag(key, 1, valueSize).SerializedItem); err != nil {
			tb.Fatalf("seed: %v", err)
		}
	}
	if err := c.Flush(); err != nil {
		tb.Fatalf("seed flush: %v", err)
	}
	for range count {
		if _, err := c.Receive(); err != nil {
			tb.Fatalf("seed recv: %v", err)
		}
	}
}

type workload struct {
	prefix    string
	mode      UpsertMode
	writers   int
	valueSize int
	itemCount int
	rtt       time.Duration
}

func runWorkload(b *testing.B, wl workload,
	body func(w int, store *redisDataStoreImpl, next func() (int, bool), rec func(time.Duration, bool, error))) {
	b.Helper()

	// Seeding and stats always go direct; only the measured traffic pays the injected round trip.
	if err := clearTestData(wl.prefix); err != nil {
		b.Fatalf("clear: %v", err)
	}
	// The seed data can be hundreds of megabytes, so do not leave it on the server.
	defer func() {
		if err := clearTestData(wl.prefix); err != nil {
			b.Errorf("clear: %v", err)
		}
	}()

	url := redisURL
	var proxy *delayProxy
	if wl.rtt > 0 {
		host := strings.TrimPrefix(redisURL, "redis://")
		proxy = startDelayProxy(b, host, wl.rtt)
		defer proxy.stop()
		url = proxy.url()
	}

	direct := benchStoreAt(wl.prefix, redisURL, wl.mode)
	defer direct.Close() //nolint:errcheck // benchmark cleanup
	if wl.itemCount > 0 {
		seedItems(b, direct, wl.itemCount, wl.valueSize)
	}

	stores := make([]*redisDataStoreImpl, wl.writers)
	for i := range stores {
		stores[i] = benchStoreAt(wl.prefix, url, wl.mode)
	}
	defer func() {
		for _, s := range stores {
			_ = s.Close()
		}
	}()

	var counter atomic.Int64
	next := func() (int, bool) {
		i := int(counter.Add(1)) - 1
		return i, i < b.N
	}

	t := &tally{durs: make([][]time.Duration, wl.writers)}
	var wg sync.WaitGroup
	resetStats(b, redisURL)
	b.ResetTimer()
	for w := range wl.writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			body(w, stores[w], next, func(d time.Duration, updated bool, err error) {
				t.record(w, d, updated, err)
			})
		}(w)
	}
	wg.Wait()
	b.StopTimer()
	t.report(b, totalCommands(b, redisURL))
}

func benchStoreAt(prefix, url string, mode UpsertMode) *redisDataStoreImpl {
	return newRedisDataStoreImpl(
		builderOptions{prefix: prefix, url: url, upsertMode: mode},
		ldlog.NewDisabledLoggers(),
	)
}

// timed runs one update and hands the duration plus the outcome to the recorder.
func timed(rec func(time.Duration, bool, error), fn func() (bool, error)) {
	start := time.Now()
	updated, err := fn()
	rec(time.Since(start), updated, err)
}

var valueSizes = []struct {
	name string
	n    int
}{
	{"512B", 512}, {"2KiB", 2 << 10}, {"4KiB", 4 << 10}, {"8KiB", 8 << 10}, {"16KiB", 16 << 10},
}

// ---------------------------------------------------------------------------------------------
// Steady state. Every SDK instance receives the same stream event and writes the same item at the
// same version. One write wins and the rest are superseded. The common case.
// ---------------------------------------------------------------------------------------------

func benchFanout(b *testing.B, mode UpsertMode, size int, writers int, rtt time.Duration) {
	kind := ldstoreimpl.Features()
	runWorkload(b, workload{prefix: "bench-fanout", mode: mode, writers: writers, valueSize: size,
		itemCount: 1, rtt: rtt},
		func(w int, store *redisDataStoreImpl, next func() (int, bool), rec func(time.Duration, bool, error)) {
			for {
				i, ok := next()
				if !ok {
					return
				}
				version := 2 + i/writers
				timed(rec, func() (bool, error) {
					return store.Upsert(kind, "flag-0", serializedFlag("flag-0", version, size))
				})
			}
		})
}

func BenchmarkUpsertRedundantFanout(b *testing.B) {
	for _, size := range valueSizes {
		for _, writers := range []int{4, 16} {
			for _, m := range upsertModes {
				b.Run(fmt.Sprintf("value=%s/writers=%d/mode=%s", size.name, writers, m.name),
					func(b *testing.B) { benchFanout(b, m.mode, size.n, writers, 0) })
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------
// The change burst. Many items edited at once. Every instance receives all the events but progresses
// at a different rate, so at any instant they write different items of the same kind. This is the
// case a hash-wide WATCH turns into false contention.
// ---------------------------------------------------------------------------------------------

func benchBurst(b *testing.B, mode UpsertMode, size, writers, items int, rtt time.Duration) {
	kind := ldstoreimpl.Features()
	stride := max(1, items/writers)
	runWorkload(b, workload{prefix: "bench-burst", mode: mode, writers: writers, valueSize: size,
		itemCount: items, rtt: rtt},
		func(w int, store *redisDataStoreImpl, next func() (int, bool), rec func(time.Duration, bool, error)) {
			for {
				i, ok := next()
				if !ok {
					return
				}
				idx := (i + w*stride) % items
				version := 2 + (i+w*stride)/items
				key := fmt.Sprintf("flag-%d", idx)
				timed(rec, func() (bool, error) {
					return store.Upsert(kind, key, serializedFlag(key, version, size))
				})
			}
		})
}

func BenchmarkUpsertItemBurst(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"512B", 512}, {"2KiB", 2 << 10}, {"16KiB", 16 << 10}} {
		for _, writers := range []int{4, 16} {
			for _, m := range upsertModes {
				b.Run(fmt.Sprintf("value=%s/writers=%d/mode=%s", size.name, writers, m.name),
					func(b *testing.B) { benchBurst(b, m.mode, size.n, writers, 200, 0) })
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------
// The worst case for the script. Every writer races one item with globally increasing versions, so
// every attempt genuinely wants to write and genuinely conflicts. Value size matters here: the script
// sends the payload twice per attempt, the expected value and the replacement, so this sweep locates
// the size at which that stops being worth it.
// ---------------------------------------------------------------------------------------------

func benchSameItem(b *testing.B, mode UpsertMode, size, writers int, rtt time.Duration) {
	kind := ldstoreimpl.Features()
	runWorkload(b, workload{prefix: "bench-samerace", mode: mode, writers: writers, valueSize: size,
		itemCount: 1, rtt: rtt},
		func(w int, store *redisDataStoreImpl, next func() (int, bool), rec func(time.Duration, bool, error)) {
			for {
				i, ok := next()
				if !ok {
					return
				}
				timed(rec, func() (bool, error) {
					return store.Upsert(kind, "flag-0", serializedFlag("flag-0", 2+i, size))
				})
			}
		})
}

func BenchmarkUpsertSameItemRace(b *testing.B) {
	for _, size := range valueSizes {
		for _, writers := range []int{4, 16} {
			for _, m := range upsertModes {
				b.Run(fmt.Sprintf("value=%s/writers=%d/mode=%s", size.name, writers, m.name),
					func(b *testing.B) { benchSameItem(b, m.mode, size.n, writers, 0) })
			}
		}
	}
}

// ---------------------------------------------------------------------------------------------
// A stream reconnects and rewrites everything with Init while other instances keep updating. Init
// replaces the whole hash for a kind, so under a hash-wide WATCH it invalidates every in-flight
// update. The number of items matters, because deleting a large hash takes time proportional to its
// size, which lengthens the window.
// ---------------------------------------------------------------------------------------------

func benchDuringInit(b *testing.B, mode UpsertMode, size, items int, rtt time.Duration) {
	kind := ldstoreimpl.Features()
	const writers = 8
	const initEvery = 50
	runWorkload(b, workload{prefix: "bench-init", mode: mode, writers: writers, valueSize: size,
		itemCount: items, rtt: rtt},
		func(w int, store *redisDataStoreImpl, next func() (int, bool), rec func(time.Duration, bool, error)) {
			descriptors := make([]ldstoretypes.KeyedSerializedItemDescriptor, items)
			for i := range descriptors {
				key := fmt.Sprintf("flag-%d", i)
				descriptors[i] = ldstoretypes.KeyedSerializedItemDescriptor{
					Key: key, Item: serializedFlag(key, 1, size)}
			}
			allData := []ldstoretypes.SerializedCollection{{Kind: kind, Items: descriptors}}

			for {
				i, ok := next()
				if !ok {
					return
				}
				if w == 0 && i%initEvery == 0 {
					if err := store.Init(allData); err != nil {
						b.Errorf("init: %v", err)
					}
					continue
				}
				idx := (i + w*(items/writers)) % items
				key := fmt.Sprintf("flag-%d", idx)
				timed(rec, func() (bool, error) {
					return store.Upsert(kind, key, serializedFlag(key, 2+i, size))
				})
			}
		})
}

func BenchmarkUpsertDuringInit(b *testing.B) {
	for _, items := range []int{200, 5000} {
		for _, m := range upsertModes {
			b.Run(fmt.Sprintf("items=%d/mode=%s", items, m.name),
				func(b *testing.B) { benchDuringInit(b, m.mode, 2<<10, items, 0) })
		}
	}
}

// ---------------------------------------------------------------------------------------------
// Over a network. Loopback hides the round-trip difference between the modes, so repeat the decisive
// configurations with an injected round-trip time. Sleep granularity makes the injected delay
// approximate; what matters is the ratio between the two modes at the same setting.
// ---------------------------------------------------------------------------------------------

func BenchmarkUpsertOverNetwork(b *testing.B) {
	for _, rtt := range []time.Duration{250 * time.Microsecond, time.Millisecond} {
		tag := fmt.Sprintf("rtt=%s", rtt)
		for _, m := range upsertModes {
			b.Run(fmt.Sprintf("%s/scenario=fanout/mode=%s", tag, m.name),
				func(b *testing.B) { benchFanout(b, m.mode, 2<<10, 16, rtt) })
			b.Run(fmt.Sprintf("%s/scenario=burst/mode=%s", tag, m.name),
				func(b *testing.B) { benchBurst(b, m.mode, 2<<10, 16, 200, rtt) })
			b.Run(fmt.Sprintf("%s/scenario=hot-item-16KiB/mode=%s", tag, m.name),
				func(b *testing.B) { benchSameItem(b, m.mode, 16<<10, 16, rtt) })
			b.Run(fmt.Sprintf("%s/scenario=init/mode=%s", tag, m.name),
				func(b *testing.B) { benchDuringInit(b, m.mode, 2<<10, 200, rtt) })
		}
	}
}
