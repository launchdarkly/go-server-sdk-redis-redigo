package ldredis

import (
	"fmt"

	r "github.com/gomodule/redigo/redis"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
)

const (
	// DefaultURL is the default value for StoreBuilder.URL.
	DefaultURL = "redis://localhost:6379"
	// DefaultPrefix is the default value for StoreBuilder.Prefix.
	DefaultPrefix = "launchdarkly"
)

// DataStore returns a configurable builder for a Redis-backed persistent data store.
//
// This is for the main data store that holds feature flag data. To configure a data store for
// Big Segments, use [BigSegmentStore] instead.
//
// You can use methods of the builder to specify any non-default Redis options you may want,
// before passing the builder to [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.PersistentDataStore].
// In this example, the store is configured to use a Redis host called "host1":
//
//	config.DataStore = ldcomponents.PersistentDataStore(
//		ldredis.DataStore().HostAndPort("host1", 6379))
//
// Note that the SDK also has its own options related to data storage that are configured
// at a different level, because they are independent of what database is being used. For
// instance, the builder returned by [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.PersistentDataStore]
// has options for caching:
//
//	config.DataStore = ldcomponents.PersistentDataStore(
//		ldredis.DataStore().HostAndPort("host1", 6379),
//	).CacheSeconds(15)
func DataStore() *StoreBuilder[subsystems.PersistentDataStore] {
	return &StoreBuilder[subsystems.PersistentDataStore]{
		builderOptions: builderOptions{
			prefix: DefaultPrefix,
			url:    DefaultURL,
		},
		factory: createPersistentDataStore,
	}
}

// BigSegmentStore returns a configurable builder for a Redis-backed Big Segment store.
//
// You can use methods of the builder to specify any non-default Redis options you may want,
// before passing the builder to [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.BigSegments].
// In this example, the store is configured to use a Redis host called "host2":
//
//	config.BigSegments = ldcomponents.BigSegments(
//		ldredis.BigSegmentStore().HostAndPort("host2", 6379))
//
// Note that the SDK also has its own options related to Big Segments that are configured
// at a different level, because they are independent of what database is being used. For
// instance, the builder returned by [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.BigSegments]
// has an option for the status polling interval:
//
//	config.BigSegments = ldcomponents.BigSegments(
//		ldredis.BigSegmentStore().HostAndPort("host2", 6379),
//	).StatusPollInterval(time.Second * 30)
func BigSegmentStore() *StoreBuilder[subsystems.BigSegmentStore] {
	return &StoreBuilder[subsystems.BigSegmentStore]{
		builderOptions: builderOptions{
			prefix: DefaultPrefix,
			url:    DefaultURL,
		},
		factory: createBigSegmentStore,
	}
}

// StoreBuilder is a builder for configuring the Redis-based persistent data store and/or Big
// Segment store.
//
// Both [DataStore] and [BigSegmentStore] return instances of this type. You can use methods of the
// builder to specify any ny non-default Redis options you may want, before passing the builder to
// either [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.PersistentDataStore] or
// [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.BigSegments] as appropriate. The two types
// of stores are independent of each other; you do not need a Big Segment store if you are not using
// the Big Segments feature, and you do not need to use the same database for both.
//
// In this example, the main data store uses a Redis host called "host1", and the Big Segment
// store uses a Redis host called "host2":
//
//     config.DataStore = ldcomponents.PersistentDataStore(
//         ldredis.DataStore().URL("redis://host1:6379")
//     config.BigSegments = ldcomponents.BigSegments(
//         ldredis.DataStore().URL("redis://host2:6379")
//
// Note that the SDK also has its own options related to data storage that are configured
// at a different level, because they are independent of what database is being used. For
// instance, the builder returned by [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.PersistentDataStore]
// has options for caching:
//
//	config.DataStore = ldcomponents.PersistentDataStore(
//		ldredis.DataStore().HostAndPort("host1", 6379),
//	).CacheSeconds(15)
type StoreBuilder[T any] struct {
	builderOptions builderOptions
	factory        func(*StoreBuilder[T], subsystems.ClientContext) (T, error)
}

type builderOptions struct {
	prefix      string
	pool        Pool
	url         string
	dialOptions []r.DialOption
}

// Prefix specifies a string that should be prepended to all Redis keys used by the data store.
// A colon will be added to this automatically. If this is unspecified or empty, [DefaultPrefix] will be used.
func (b *StoreBuilder[T]) Prefix(prefix string) *StoreBuilder[T] {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	b.builderOptions.prefix = prefix
	return b
}

// URL specifies the Redis host URL. If not specified, the default value is [DefaultURL].
//
// Note that some Redis client features can also be specified as part of the URL: Redigo supports
// the redis:// syntax (https://www.iana.org/assignments/uri-schemes/prov/redis), which can include a
// password and a database number, as well as rediss://
// (https://www.iana.org/assignments/uri-schemes/prov/rediss), which enables TLS.
func (b *StoreBuilder[T]) URL(url string) *StoreBuilder[T] {
	if url == "" {
		url = DefaultURL
	}
	b.builderOptions.url = url
	return b
}

// HostAndPort is a shortcut for specifying the Redis host address as a hostname and port.
func (b *StoreBuilder[T]) HostAndPort(host string, port int) *StoreBuilder[T] {
	return b.URL(fmt.Sprintf("redis://%s:%d", host, port))
}

// Pool specifies that the data store should use a specific connection pool configuration. If not
// specified, it will create a default configuration (see package description). Specifying this
// option will cause any address specified with URL or HostAndPort to be ignored.
//
// If you only need to change basic connection options such as providing a password, it is
// simpler to use DialOptions.
//
// Use PoolInterface if you want to provide your own implementation of a connection pool.
func (b *StoreBuilder[T]) Pool(pool *r.Pool) *StoreBuilder[T] {
	b.builderOptions.pool = pool
	return b
}

// PoolInterface is equivalent to Pool, but uses an interface type rather than a concrete
// implementation type. This allows implementation of custom behaviors for connection management.
func (b *StoreBuilder[T]) PoolInterface(pool Pool) *StoreBuilder[T] {
	b.builderOptions.pool = pool
	return b
}

// DialOptions specifies any of the advanced Redis connection options supported by Redigo, such as
// DialPassword.
//
//     import (
//         redigo "github.com/garyburd/redigo/redis"
//         ldredis "github.com/launchdarkly/go-server-sdk-redis-redigo/v2"
//     )
//     config.DataSource = ldcomponents.PersistentDataStore(
//         ldredis.DataStore().DialOptions(redigo.DialPassword("verysecure123")),
//     )
// Note that some Redis client features can also be specified as part of the URL: see  URL().
func (b *StoreBuilder[T]) DialOptions(options ...r.DialOption) *StoreBuilder[T] {
	b.builderOptions.dialOptions = options
	return b
}

// Build is called internally by the SDK.
func (b *StoreBuilder[T]) Build(context subsystems.ClientContext) (T, error) {
	return b.factory(b, context)
}

// DescribeConfiguration is used internally by the SDK to inspect the configuration.
func (b *StoreBuilder[T]) DescribeConfiguration() ldvalue.Value {
	return ldvalue.String("Redis")
}

// Pool is an interface representing a Redis connection pool.
//
// The methods of this interface are the same as the basic methods of the Pool type in
// the Redigo client. Any type implementing the interface can be passed to
// StoreBuilder.PoolInterface to provide custom connection behavior.
type Pool interface {
	// Get obtains a Redis connection.
	//
	// See: https://pkg.go.dev/github.com/gomodule/redigo/redis#Pool.Get
	Get() r.Conn

	// Close releases the resources used by the pool.
	//
	// See: https://pkg.go.dev/github.com/gomodule/redigo/redis#Pool.Close
	Close() error
}

func createPersistentDataStore(
	builder *StoreBuilder[subsystems.PersistentDataStore],
	clientContext subsystems.ClientContext,
) (subsystems.PersistentDataStore, error) {
	store := newRedisDataStoreImpl(builder.builderOptions, clientContext.GetLogging().Loggers)
	return store, nil
}

func createBigSegmentStore(
	builder *StoreBuilder[subsystems.BigSegmentStore],
	clientContext subsystems.ClientContext,
) (subsystems.BigSegmentStore, error) {
	store := newRedisBigSegmentStoreImpl(builder.builderOptions, clientContext.GetLogging().Loggers)
	return store, nil
}
