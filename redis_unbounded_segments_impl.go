package ldredis

import (
	"fmt"
	"strconv"

	r "github.com/gomodule/redigo/redis"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldlog"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldtime"
	"gopkg.in/launchdarkly/go-server-sdk.v5/interfaces"
	"gopkg.in/launchdarkly/go-server-sdk.v5/ldcomponents/ldstoreimpl"
)

// Internal implementation of the UnboundedSegmentStore interface for Redis.
type redisUnboundedSegmentStoreImpl struct {
	prefix  string
	pool    *r.Pool
	loggers ldlog.Loggers
}

func newRedisUnboundedSegmentStoreImpl(
	builder *DataStoreBuilder,
	loggers ldlog.Loggers,
) *redisUnboundedSegmentStoreImpl {
	impl := &redisUnboundedSegmentStoreImpl{
		prefix:  builder.prefix,
		pool:    builder.pool,
		loggers: loggers,
	}
	impl.loggers.SetPrefix("RedisUnboundedSegmentStore:")

	if impl.pool == nil {
		impl.loggers.Infof("Using URL: %s", builder.url)
		impl.pool = newPool(builder.url, builder.dialOptions)
	}
	return impl
}

func (store *redisUnboundedSegmentStoreImpl) GetMetadata() (interfaces.UnboundedSegmentStoreMetadata, error) {
	c := store.getConn()
	defer c.Close() //nolint:errcheck

	valueStr, err := r.String(c.Do("GET", unboundedSegmentsSyncTimeKey(store.prefix)))
	if err != nil {
		return interfaces.UnboundedSegmentStoreMetadata{}, err
	}
	value, err := strconv.ParseUint(valueStr, 10, 64)
	if err != nil {
		return interfaces.UnboundedSegmentStoreMetadata{}, err
	}

	return interfaces.UnboundedSegmentStoreMetadata{
		LastUpToDate: ldtime.UnixMillisecondTime(value),
	}, nil
}

func (store *redisUnboundedSegmentStoreImpl) GetUserMembership(
	userHashKey string,
) (interfaces.UnboundedSegmentMembership, error) {
	c := store.getConn()
	defer c.Close() //nolint:errcheck

	includedKeys, err := r.Strings(c.Do("SMEMBERS", unboundedSegmentsIncludeKey(store.prefix, userHashKey)))
	if err != nil {
		return nil, err
	}
	excludedKeys, err := r.Strings(c.Do("SMEMBERS", unboundedSegmentsExcludeKey(store.prefix, userHashKey)))
	if err != nil {
		return nil, err
	}

	return ldstoreimpl.NewUnboundedSegmentMembershipFromKeys(includedKeys, excludedKeys), nil
}

func (store *redisUnboundedSegmentStoreImpl) Close() error {
	return store.pool.Close()
}

func (store *redisUnboundedSegmentStoreImpl) getConn() r.Conn {
	return store.pool.Get()
}

func unboundedSegmentsSyncTimeKey(prefix string) string {
	return fmt.Sprintf("%s:unbounded_segments_synchronized_on", prefix)
}

func unboundedSegmentsIncludeKey(prefix, userHashKey string) string {
	return fmt.Sprintf("%s:unbounded_segment_include:%s", prefix, userHashKey)
}

func unboundedSegmentsExcludeKey(prefix, userHashKey string) string {
	return fmt.Sprintf("%s:unbounded_segment_exclude:%s", prefix, userHashKey)
}
