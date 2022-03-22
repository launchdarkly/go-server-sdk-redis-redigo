package ldredis

import (
	"fmt"

	r "github.com/gomodule/redigo/redis"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v6/interfaces"
)

const (
	// DefaultURL is the default URL for connecting to Redis, if you use
	// NewRedisDataStoreWithDefaults. You can specify otherwise with the RedisURL option.
	// If you are using the other constructors, you must specify the URL explicitly.
	DefaultURL = "redis://localhost:6379"
	// DefaultPrefix is a string that is prepended (along with a colon) to all Redis keys used
	// by the data store. You can change this value with the Prefix() option for
	// NewRedisDataStoreWithDefaults, or with the "prefix" parameter to the other constructors.
	DefaultPrefix = "launchdarkly"
)

// DataStore returns a configurable builder for a Redis-backed data store.
//
// This can be used either for the main data store that holds feature flag data, or for the big
// segment store, or both. If you are using both, they do not have to have the same parameters. For
// instance, in this example the main data store uses a Redis host called "host1" and the big
// segment store uses a Redis host called "host2":
//
//     config.DataStore = ldcomponents.PersistentDataStore(
//         ldredis.DataStore().HostAndPort("host1", 6379))
//     config.BigSegments = ldcomponents.BigSegments(
//         ldredis.DataStore().HostAndPort("host2", 6379))
//
// Note that the builder is passed to one of two methods, PersistentDataStore or BigSegments,
// depending on the context in which it is being used. This is because each of those contexts has
// its own additional configuration options that are unrelated to the Redis options.
func DataStore() *DataStoreBuilder {
	return &DataStoreBuilder{
		prefix: DefaultPrefix,
		url:    DefaultURL,
	}
}

// DataStoreBuilder is a builder for configuring the Redis-based persistent data store.
//
// Obtain an instance of this type by calling DataStore(). After calling its methods to specify any
// desired custom settings, wrap it in a PersistentDataStoreBuilder by calling
// ldcomponents.PersistentDataStore(), and then store this in the SDK configuration's DataStore field.
//
// Builder calls can be chained, for example:
//
//     config.DataStore = ldcomponents.PersistentDataStore(
//         ldredis.DataStore().URL("redis://hostname").Prefix("prefix"))
//
// You do not need to call the builder's CreatePersistentDataStore() method yourself to build the
// actual data store; that will be done by the SDK.
type DataStoreBuilder struct {
	prefix      string
	pool        Pool
	url         string
	dialOptions []r.DialOption
}

// Prefix specifies a string that should be prepended to all Redis keys used by the data store.
// A colon will be added to this automatically. If this is unspecified or empty, DefaultPrefix will be used.
func (b *DataStoreBuilder) Prefix(prefix string) *DataStoreBuilder {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	b.prefix = prefix
	return b
}

// URL specifies the Redis host URL. If not specified, the default value is DefaultURL.
//
// Note that some Redis client features can also be specified as part of the URL: Redigo supports
// the redis:// syntax (https://www.iana.org/assignments/uri-schemes/prov/redis), which can include a
// password and a database number, as well as rediss://
// (https://www.iana.org/assignments/uri-schemes/prov/rediss), which enables TLS.
func (b *DataStoreBuilder) URL(url string) *DataStoreBuilder {
	if url == "" {
		url = DefaultURL
	}
	b.url = url
	return b
}

// HostAndPort is a shortcut for specifying the Redis host address as a hostname and port.
func (b *DataStoreBuilder) HostAndPort(host string, port int) *DataStoreBuilder {
	return b.URL(fmt.Sprintf("redis://%s:%d", host, port))
}

// Pool specifies that the data store should use a specific connection pool configuration. If not
// specified, it will create a default configuration (see package description). Specifying this
// option will cause any address specified with URL() or HostAndPort() to be ignored.
//
// If you only need to change basic connection options such as providing a password, it is
// simpler to use DialOptions().
//
// Use PoolInterface() if you want to provide your own implementation of a connection pool.
func (b *DataStoreBuilder) Pool(pool *r.Pool) *DataStoreBuilder {
	b.pool = pool
	return b
}

// PoolInterface is equivalent to Pool, but uses an interface type rather than a concrete
// implementation type. This allows implementation of custom behaviors for connection management.
func (b *DataStoreBuilder) PoolInterface(pool Pool) *DataStoreBuilder {
	b.pool = pool
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
func (b *DataStoreBuilder) DialOptions(options ...r.DialOption) *DataStoreBuilder {
	b.dialOptions = options
	return b
}

// CreatePersistentDataStore is called internally by the SDK to create a data store implementation object.
func (b *DataStoreBuilder) CreatePersistentDataStore(
	context interfaces.ClientContext,
) (interfaces.PersistentDataStore, error) {
	store := newRedisDataStoreImpl(b, context.GetLogging().Loggers)
	return store, nil
}

// CreateBigSegmentStore is called internally by the SDK to create a data store implementation object.
func (b *DataStoreBuilder) CreateBigSegmentStore(
	context interfaces.ClientContext,
) (interfaces.BigSegmentStore, error) {
	store := newRedisBigSegmentStoreImpl(b, context.GetLogging().Loggers)
	return store, nil
}

// DescribeConfiguration is used internally by the SDK to inspect the configuration.
func (b *DataStoreBuilder) DescribeConfiguration() ldvalue.Value {
	return ldvalue.String("Redis")
}

// Pool is an interface representing a Redis connection pool.
//
// The methods of this interface are the same as the basic methods of the Pool type in
// the Redigo client. Any type implementing the interface can be passed to
// DataStoreBuilder.PoolInterface() to provide custom connection behavior.
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
