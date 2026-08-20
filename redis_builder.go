package ldredis

import (
	"fmt"

	r "github.com/gomodule/redigo/redis"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
)

const (
	// DefaultURL is the default value for the URL option of either builder.
	DefaultURL = "redis://localhost:6379"
	// DefaultPrefix is the default value for the Prefix option of either builder.
	DefaultPrefix = "launchdarkly"
)

// builderOptions holds the Redis connection settings that both builders accept. The setters are
// shared by both builders so that each option behaves identically no matter which builder it was
// set on.
type builderOptions struct {
	prefix      string
	pool        Pool
	url         string
	dialOptions []r.DialOption
}

func defaultBuilderOptions() builderOptions {
	return builderOptions{
		prefix: DefaultPrefix,
		url:    DefaultURL,
	}
}

func (o *builderOptions) setPrefix(prefix string) {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	o.prefix = prefix
}

func (o *builderOptions) setURL(url string) {
	if url == "" {
		url = DefaultURL
	}
	o.url = url
}

func (o *builderOptions) setHostAndPort(host string, port int) {
	o.setURL(fmt.Sprintf("redis://%s:%d", host, port))
}

func (o *builderOptions) setPool(pool Pool) {
	o.pool = pool
}

func (o *builderOptions) setDialOptions(options []r.DialOption) {
	o.dialOptions = options
}

// DataStoreBuilder is a builder for configuring the Redis-based persistent data store.
//
// Obtain an instance of this type by calling [DataStore]. After calling its methods to specify any
// non-default Redis options you may want, pass it to
// [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.PersistentDataStore] and store the result
// in the SDK configuration's DataStore field.
//
// To configure a store for Big Segments instead, use [BigSegmentStore]. The two kinds of store are
// independent of each other. You do not need a Big Segment store if you are not using the Big
// Segments feature, and you do not need to use the same database for both.
type DataStoreBuilder struct {
	opts builderOptions
}

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
func DataStore() *DataStoreBuilder {
	return &DataStoreBuilder{opts: defaultBuilderOptions()}
}

// Prefix specifies a string that should be prepended to all Redis keys used by the data store.
// A colon will be added to this automatically. If this is unspecified or empty, [DefaultPrefix] will be used.
func (b *DataStoreBuilder) Prefix(prefix string) *DataStoreBuilder {
	b.opts.setPrefix(prefix)
	return b
}

// URL specifies the Redis host URL. If not specified, the default value is [DefaultURL].
//
// Note that some Redis client features can also be specified as part of the URL: Redigo supports
// the redis:// syntax (https://www.iana.org/assignments/uri-schemes/prov/redis), which can include a
// password and a database number, as well as rediss://
// (https://www.iana.org/assignments/uri-schemes/prov/rediss), which enables TLS.
func (b *DataStoreBuilder) URL(url string) *DataStoreBuilder {
	b.opts.setURL(url)
	return b
}

// HostAndPort is a shortcut for specifying the Redis host address as a hostname and port.
func (b *DataStoreBuilder) HostAndPort(host string, port int) *DataStoreBuilder {
	b.opts.setHostAndPort(host, port)
	return b
}

// Pool specifies that the data store should use a specific connection pool configuration. If not
// specified, it will create a default configuration (see package description). Specifying this
// option will cause any address specified with URL or HostAndPort to be ignored.
//
// If you only need to change basic connection options such as providing a password, it is
// simpler to use DialOptions.
//
// Use PoolInterface if you want to provide your own implementation of a connection pool.
func (b *DataStoreBuilder) Pool(pool *r.Pool) *DataStoreBuilder {
	b.opts.setPool(pool)
	return b
}

// PoolInterface is equivalent to Pool, but uses an interface type rather than a concrete
// implementation type. This allows implementation of custom behaviors for connection management.
func (b *DataStoreBuilder) PoolInterface(pool Pool) *DataStoreBuilder {
	b.opts.setPool(pool)
	return b
}

// DialOptions specifies any of the advanced Redis connection options supported by Redigo, such as
// DialPassword.
//
//	import (
//	    redigo "github.com/gomodule/redigo/redis"
//	    ldredis "github.com/launchdarkly/go-server-sdk-redis-redigo/v4"
//	)
//	config.DataStore = ldcomponents.PersistentDataStore(
//	    ldredis.DataStore().DialOptions(redigo.DialPassword("verysecure123")),
//	)
//
// Note that some Redis client features can also be specified as part of the URL: see URL.
func (b *DataStoreBuilder) DialOptions(options ...r.DialOption) *DataStoreBuilder {
	b.opts.setDialOptions(options)
	return b
}

// Build is called internally by the SDK.
func (b *DataStoreBuilder) Build(context subsystems.ClientContext) (subsystems.PersistentDataStore, error) {
	return newRedisDataStoreImpl(b.opts, context.GetLogging().Loggers), nil
}

// DescribeConfiguration is used internally by the SDK to inspect the configuration.
func (b *DataStoreBuilder) DescribeConfiguration() ldvalue.Value {
	return ldvalue.String("Redis")
}

// BigSegmentStoreBuilder is a builder for configuring the Redis-based Big Segment store.
//
// Obtain an instance of this type by calling [BigSegmentStore]. After calling its methods to specify
// any non-default Redis options you may want, pass it to
// [github.com/launchdarkly/go-server-sdk/v7/ldcomponents.BigSegments] and store the result in the
// SDK configuration's BigSegments field.
//
// To configure the main data store that holds feature flag data, use [DataStore] instead. The two
// kinds of store are independent of each other, and do not need to use the same database.
type BigSegmentStoreBuilder struct {
	opts builderOptions
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
func BigSegmentStore() *BigSegmentStoreBuilder {
	return &BigSegmentStoreBuilder{opts: defaultBuilderOptions()}
}

// Prefix specifies a string that should be prepended to all Redis keys used by the Big Segment
// store. A colon will be added to this automatically. If this is unspecified or empty,
// [DefaultPrefix] will be used.
func (b *BigSegmentStoreBuilder) Prefix(prefix string) *BigSegmentStoreBuilder {
	b.opts.setPrefix(prefix)
	return b
}

// URL specifies the Redis host URL. If not specified, the default value is [DefaultURL].
//
// Note that some Redis client features can also be specified as part of the URL: Redigo supports
// the redis:// syntax (https://www.iana.org/assignments/uri-schemes/prov/redis), which can include a
// password and a database number, as well as rediss://
// (https://www.iana.org/assignments/uri-schemes/prov/rediss), which enables TLS.
func (b *BigSegmentStoreBuilder) URL(url string) *BigSegmentStoreBuilder {
	b.opts.setURL(url)
	return b
}

// HostAndPort is a shortcut for specifying the Redis host address as a hostname and port.
func (b *BigSegmentStoreBuilder) HostAndPort(host string, port int) *BigSegmentStoreBuilder {
	b.opts.setHostAndPort(host, port)
	return b
}

// Pool specifies that the Big Segment store should use a specific connection pool configuration. If
// not specified, it will create a default configuration (see package description). Specifying this
// option will cause any address specified with URL or HostAndPort to be ignored.
//
// If you only need to change basic connection options such as providing a password, it is
// simpler to use DialOptions.
//
// Use PoolInterface if you want to provide your own implementation of a connection pool.
func (b *BigSegmentStoreBuilder) Pool(pool *r.Pool) *BigSegmentStoreBuilder {
	b.opts.setPool(pool)
	return b
}

// PoolInterface is equivalent to Pool, but uses an interface type rather than a concrete
// implementation type. This allows implementation of custom behaviors for connection management.
func (b *BigSegmentStoreBuilder) PoolInterface(pool Pool) *BigSegmentStoreBuilder {
	b.opts.setPool(pool)
	return b
}

// DialOptions specifies any of the advanced Redis connection options supported by Redigo, such as
// DialPassword.
//
//	import (
//	    redigo "github.com/gomodule/redigo/redis"
//	    ldredis "github.com/launchdarkly/go-server-sdk-redis-redigo/v4"
//	)
//	config.BigSegments = ldcomponents.BigSegments(
//	    ldredis.BigSegmentStore().DialOptions(redigo.DialPassword("verysecure123")),
//	)
//
// Note that some Redis client features can also be specified as part of the URL: see URL.
func (b *BigSegmentStoreBuilder) DialOptions(options ...r.DialOption) *BigSegmentStoreBuilder {
	b.opts.setDialOptions(options)
	return b
}

// Build is called internally by the SDK.
func (b *BigSegmentStoreBuilder) Build(context subsystems.ClientContext) (subsystems.BigSegmentStore, error) {
	return newRedisBigSegmentStoreImpl(b.opts, context.GetLogging().Loggers), nil
}

// DescribeConfiguration is used internally by the SDK to inspect the configuration.
func (b *BigSegmentStoreBuilder) DescribeConfiguration() ldvalue.Value {
	return ldvalue.String("Redis")
}

// Pool is an interface representing a Redis connection pool.
//
// The methods of this interface are the same as the basic methods of the Pool type in
// the Redigo client. Any type implementing the interface can be passed to
// DataStoreBuilder.PoolInterface or BigSegmentStoreBuilder.PoolInterface to provide custom
// connection behavior.
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
