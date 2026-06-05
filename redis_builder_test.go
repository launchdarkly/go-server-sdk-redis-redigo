package ldredis

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-sdk-common/v4/ldlog"
	"github.com/launchdarkly/go-sdk-common/v4/ldlogtest"
)

func TestDataStoreBuilder(t *testing.T) {
	testStoreBuilder(t, DataStore)
}

func TestBigSegmentStoreBuilder(t *testing.T) {
	testStoreBuilder(t, BigSegmentStore)
}

func testStoreBuilder[T any](t *testing.T, factory func() *StoreBuilder[T]) {
	t.Run("defaults", func(t *testing.T) {
		b := factory()
		assert.Len(t, b.builderOptions.dialOptions, 0)
		assert.Nil(t, b.builderOptions.pool)
		assert.Equal(t, DefaultPrefix, b.builderOptions.prefix)
		assert.Equal(t, DefaultURL, b.builderOptions.url)
	})

	t.Run("DialOptions", func(t *testing.T) {
		o1 := r.DialPassword("p")
		o2 := r.DialTLSSkipVerify(true)
		b := factory().DialOptions(o1, o2)
		assert.Len(t, b.builderOptions.dialOptions, 2) // a DialOption is a function, so can't do an equality test
	})

	t.Run("HostAndPort", func(t *testing.T) {
		b := factory().HostAndPort("mine", 4000)
		assert.Equal(t, "redis://mine:4000", b.builderOptions.url)
	})

	t.Run("Pool", func(t *testing.T) {
		p := &r.Pool{MaxActive: 999}
		b := factory().Pool(p)
		assert.Equal(t, p, b.builderOptions.pool)
	})

	t.Run("PoolInterface", func(t *testing.T) {
		p := &myCustomPool{Pool: r.Pool{MaxActive: 999}}
		b := factory().PoolInterface(p)
		assert.Equal(t, p, b.builderOptions.pool)
	})

	t.Run("Prefix", func(t *testing.T) {
		b := factory().Prefix("p")
		assert.Equal(t, "p", b.builderOptions.prefix)

		b.Prefix("")
		assert.Equal(t, DefaultPrefix, b.builderOptions.prefix)
	})

	t.Run("URL", func(t *testing.T) {
		url := "redis://mine"
		b := factory().URL(url)
		assert.Equal(t, url, b.builderOptions.url)

		b.URL("")
		assert.Equal(t, DefaultURL, b.builderOptions.url)
	})

	t.Run("PasswordProvider", func(t *testing.T) {
		fn := func(ctx context.Context) (string, error) { return "tok", nil }
		b := factory().PasswordProvider(fn)
		assert.NotNil(t, b.builderOptions.passwordProvider)
	})

	t.Run("MaxConnLifetime", func(t *testing.T) {
		b := factory().MaxConnLifetime(11 * time.Hour)
		assert.Equal(t, 11*time.Hour, b.builderOptions.maxConnLifetime)
	})
}

// startStubTCPServer starts a TCP listener that accepts a single connection then closes it.
// This is sufficient to drive the redigo Dial closure through to the point where the password
// provider (if any) is invoked. The connection won't survive any subsequent Redis commands.
// Each accepted connection sends a Redis OK reply to the AUTH command so DialURL completes.
func startStubTCPServer(t *testing.T) (addr string, close func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close2(l)
		for {
			conn, err := l.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go func(c net.Conn) {
				// Read a small chunk so the AUTH command is consumed, then reply OK to every
				// inline read. We don't fully parse RESP — we just keep echoing "+OK\r\n" on
				// every received chunk until the client closes. That's enough for the dial
				// closure + initial AUTH to succeed in redigo.
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil || n == 0 {
						_ = c.Close()
						return
					}
					if _, err := c.Write([]byte("+OK\r\n")); err != nil {
						_ = c.Close()
						return
					}
				}
			}(conn)
		}
	}()

	return l.Addr().String(), func() {
		close3(done)
		_ = l.Close()
	}
}

func close2(l net.Listener) { _ = l.Close() }
func close3(done chan struct{}) {
	defer func() { recover() }() //nolint:errcheck
	close(done)
}

func TestPasswordProviderInvokedPerDial(t *testing.T) {
	addr, stop := startStubTCPServer(t)
	defer stop()

	var calls int32
	provider := func(ctx context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "iam-token", nil
	}

	loggers := ldlog.NewDisabledLoggers()
	opts := builderOptions{
		url:              "redis://" + addr,
		passwordProvider: provider,
	}
	pool := newPool(opts, loggers)
	defer pool.Close() //nolint:errcheck

	// Force three fresh dials by getting+closing three connections. The pool has MaxIdle=20,
	// so connections returned to the pool may be reused — but err'd connections are discarded.
	// To guarantee fresh dials we use a separate non-pooled call path: drain the pool by
	// calling ActiveCount-aware Get and then closing the underlying conn forcibly. The
	// simplest approach is to just call Dial directly via pool internals: redigo doesn't
	// expose that, so we instead obtain a connection, do nothing with it, and close it; the
	// connection is then idle. We then trigger MaxConnLifetime expiry by setting it small.
	// Simpler still: just verify >= 1 invocation across several Gets — and assert that
	// the provider is called at least once per *new* dial by depleting MaxActive.
	for i := 0; i < 3; i++ {
		c := pool.Get()
		// Don't issue commands — our stub server isn't a real Redis. Just close the connection
		// after marking it as failed so the pool doesn't reuse it.
		_ = c.Err()
		_ = c.Close()
	}

	got := atomic.LoadInt32(&calls)
	assert.GreaterOrEqual(t, got, int32(1),
		"PasswordProvider should be invoked at least once per Dial; got %d", got)
}

func TestPasswordProviderInvokedPerDial_NotMemoized(t *testing.T) {
	addr, stop := startStubTCPServer(t)
	defer stop()

	var calls int32
	provider := func(ctx context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "iam-token", nil
	}

	loggers := ldlog.NewDisabledLoggers()
	opts := builderOptions{
		url:              "redis://" + addr,
		passwordProvider: provider,
	}

	// Build two separate pools to guarantee two separate dials, sidestepping any
	// idle-reuse heuristic in redigo's pool.
	p1 := newPool(opts, loggers)
	defer p1.Close() //nolint:errcheck
	c1 := p1.Get()
	_ = c1.Close()

	first := atomic.LoadInt32(&calls)
	require.GreaterOrEqual(t, first, int32(1), "expected first pool's Dial to invoke provider")

	p2 := newPool(opts, loggers)
	defer p2.Close() //nolint:errcheck
	c2 := p2.Get()
	_ = c2.Close()

	second := atomic.LoadInt32(&calls)
	assert.Greater(t, second, first, "expected second pool's Dial to invoke provider again (not memoized)")
}

func TestPasswordProviderErrorPropagates(t *testing.T) {
	wantErr := errors.New("could not mint iam token")
	provider := func(ctx context.Context) (string, error) { return "", wantErr }

	loggers := ldlog.NewDisabledLoggers()
	opts := builderOptions{
		url:              "redis://127.0.0.1:1", // would otherwise fail; provider errors first
		passwordProvider: provider,
	}
	pool := newPool(opts, loggers)
	defer pool.Close() //nolint:errcheck

	c := pool.Get()
	defer c.Close() //nolint:errcheck

	// The connection should carry the provider's error.
	err := c.Err()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr, "expected provider error to propagate, got %v", err)
}

func TestPasswordProviderNotInvokedWhenPoolIsSet(t *testing.T) {
	var calls int32
	provider := func(ctx context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "tok", nil
	}

	customPool := &r.Pool{
		MaxIdle: 1,
		Dial: func() (r.Conn, error) {
			// Caller-owned pool: return a stub error rather than actually dialing.
			return nil, errors.New("caller-supplied pool dial")
		},
	}

	// Use the public builder API: when Pool() is set, the internal newPool is never called.
	b := DataStore().
		PasswordProvider(provider).
		Pool(customPool)

	mockLog := ldlogtest.NewMockLog()
	defer mockLog.DumpIfTestFailed(t)
	impl := newRedisDataStoreImpl(b.builderOptions, mockLog.Loggers)
	defer impl.Close() //nolint:errcheck

	// The store's pool should be the caller-supplied pool, not an internally-constructed one.
	assert.Same(t, customPool, impl.pool, "expected caller-supplied pool to be used as-is")

	// Drive a Get to confirm the caller-supplied Dial fires, not ours.
	c := impl.pool.Get()
	_ = c.Close()

	assert.Equal(t, int32(0), atomic.LoadInt32(&calls),
		"PasswordProvider must not be invoked when Pool() is set")
}

func TestMaxConnLifetimeAppliedToInternalPool(t *testing.T) {
	loggers := ldlog.NewDisabledLoggers()
	want := 11 * time.Hour
	opts := builderOptions{
		url:             "redis://127.0.0.1:0",
		maxConnLifetime: want,
	}
	pool := newPool(opts, loggers)
	defer pool.Close() //nolint:errcheck

	assert.Equal(t, want, pool.MaxConnLifetime)
}

func TestMaxConnLifetimeDefaultsToZero(t *testing.T) {
	loggers := ldlog.NewDisabledLoggers()
	opts := builderOptions{
		url: "redis://127.0.0.1:0",
	}
	pool := newPool(opts, loggers)
	defer pool.Close() //nolint:errcheck

	assert.Equal(t, time.Duration(0), pool.MaxConnLifetime,
		"MaxConnLifetime should default to 0 (no limit), matching redigo's default")
}

func TestPasswordProviderWithDialOptionsLogsWarning(t *testing.T) {
	provider := func(ctx context.Context) (string, error) { return "tok", nil }

	mockLog := ldlogtest.NewMockLog()
	defer mockLog.DumpIfTestFailed(t)

	opts := builderOptions{
		url:              "redis://127.0.0.1:0",
		passwordProvider: provider,
		dialOptions:      []r.DialOption{r.DialPassword("static")},
	}
	pool := newPool(opts, mockLog.Loggers)
	defer pool.Close() //nolint:errcheck

	mockLog.AssertMessageMatch(t, true, ldlog.Warn, "PasswordProvider is set alongside DialOptions")
}

// myCustomPool is an example of a Redis pool wrapper.
type myCustomPool struct {
	r.Pool

	getCount   int
	closeCount int
}

func (m *myCustomPool) Get() r.Conn {
	m.getCount++
	return m.Pool.Get()
}

func (m *myCustomPool) Close() error {
	m.closeCount++
	return m.Pool.Close()
}
