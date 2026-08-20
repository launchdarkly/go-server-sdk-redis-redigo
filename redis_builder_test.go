package ldredis

import (
	"testing"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
)

// The SDK consumes each builder only through the ComponentConfigurer interface for its own store
// type, so these assertions cover everything the SDK requires of the builder types.
var (
	_ subsystems.ComponentConfigurer[subsystems.PersistentDataStore] = (*DataStoreBuilder)(nil)
	_ subsystems.ComponentConfigurer[subsystems.BigSegmentStore]     = (*BigSegmentStoreBuilder)(nil)
)

// builderUnderTest adapts one concrete builder type to the shared connection-option assertions.
// Each field takes and returns the builder type, so assigning a method expression to it also
// asserts that the option returns its own builder type and that chaining is preserved.
type builderUnderTest[B any] struct {
	factory       func() B
	optionsOf     func(B) builderOptions
	dialOptions   func(B, ...r.DialOption) B
	hostAndPort   func(B, string, int) B
	pool          func(B, *r.Pool) B
	poolInterface func(B, Pool) B
	prefix        func(B, string) B
	url           func(B, string) B
}

func TestDataStoreBuilder(t *testing.T) {
	testConnectionOptions(t, builderUnderTest[*DataStoreBuilder]{
		factory:       DataStore,
		optionsOf:     func(b *DataStoreBuilder) builderOptions { return b.opts },
		dialOptions:   (*DataStoreBuilder).DialOptions,
		hostAndPort:   (*DataStoreBuilder).HostAndPort,
		pool:          (*DataStoreBuilder).Pool,
		poolInterface: (*DataStoreBuilder).PoolInterface,
		prefix:        (*DataStoreBuilder).Prefix,
		url:           (*DataStoreBuilder).URL,
	})
}

func TestBigSegmentStoreBuilder(t *testing.T) {
	testConnectionOptions(t, builderUnderTest[*BigSegmentStoreBuilder]{
		factory:       BigSegmentStore,
		optionsOf:     func(b *BigSegmentStoreBuilder) builderOptions { return b.opts },
		dialOptions:   (*BigSegmentStoreBuilder).DialOptions,
		hostAndPort:   (*BigSegmentStoreBuilder).HostAndPort,
		pool:          (*BigSegmentStoreBuilder).Pool,
		poolInterface: (*BigSegmentStoreBuilder).PoolInterface,
		prefix:        (*BigSegmentStoreBuilder).Prefix,
		url:           (*BigSegmentStoreBuilder).URL,
	})
}

func testConnectionOptions[B any](t *testing.T, subject builderUnderTest[B]) {
	t.Run("defaults", func(t *testing.T) {
		opts := subject.optionsOf(subject.factory())
		assert.Len(t, opts.dialOptions, 0)
		assert.Nil(t, opts.pool)
		assert.Equal(t, DefaultPrefix, opts.prefix)
		assert.Equal(t, DefaultURL, opts.url)
	})

	t.Run("DialOptions", func(t *testing.T) {
		o1 := r.DialPassword("p")
		o2 := r.DialTLSSkipVerify(true)
		b := subject.dialOptions(subject.factory(), o1, o2)
		// a DialOption is a function, so can't do an equality test
		assert.Len(t, subject.optionsOf(b).dialOptions, 2)
	})

	t.Run("HostAndPort", func(t *testing.T) {
		b := subject.hostAndPort(subject.factory(), "mine", 4000)
		assert.Equal(t, "redis://mine:4000", subject.optionsOf(b).url)
	})

	t.Run("Pool", func(t *testing.T) {
		p := &r.Pool{MaxActive: 999}
		b := subject.pool(subject.factory(), p)
		assert.Equal(t, p, subject.optionsOf(b).pool)
	})

	t.Run("PoolInterface", func(t *testing.T) {
		p := &myCustomPool{Pool: r.Pool{MaxActive: 999}}
		b := subject.poolInterface(subject.factory(), p)
		assert.Equal(t, p, subject.optionsOf(b).pool)
	})

	t.Run("Prefix", func(t *testing.T) {
		b := subject.prefix(subject.factory(), "p")
		assert.Equal(t, "p", subject.optionsOf(b).prefix)

		b = subject.prefix(b, "")
		assert.Equal(t, DefaultPrefix, subject.optionsOf(b).prefix)
	})

	t.Run("URL", func(t *testing.T) {
		url := "redis://mine"
		b := subject.url(subject.factory(), url)
		assert.Equal(t, url, subject.optionsOf(b).url)

		b = subject.url(b, "")
		assert.Equal(t, DefaultURL, subject.optionsOf(b).url)
	})
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
