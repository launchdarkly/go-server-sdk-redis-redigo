package ldredis

import (
	"testing"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"
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
