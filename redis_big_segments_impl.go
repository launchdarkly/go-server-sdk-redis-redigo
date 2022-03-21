package ldredis

import (
	"fmt"
	"strconv"

	r "github.com/gomodule/redigo/redis"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-sdk-common/v3/ldtime"
	"github.com/launchdarkly/go-server-sdk/v6/interfaces"
	"github.com/launchdarkly/go-server-sdk/v6/ldcomponents/ldstoreimpl"
)

// Internal implementation of the BigSegmentStore interface for Redis.
type redisBigSegmentStoreImpl struct {
	prefix  string
	pool    Pool
	loggers ldlog.Loggers
}

func newRedisBigSegmentStoreImpl(
	builder *DataStoreBuilder,
	loggers ldlog.Loggers,
) *redisBigSegmentStoreImpl {
	impl := &redisBigSegmentStoreImpl{
		prefix:  builder.prefix,
		pool:    builder.pool,
		loggers: loggers,
	}
	impl.loggers.SetPrefix("RedisBigSegmentStore:")

	if impl.pool == nil {
		logRedisURL(loggers, builder.url)
		impl.pool = newPool(builder.url, builder.dialOptions)
	}
	return impl
}

func (store *redisBigSegmentStoreImpl) GetMetadata() (interfaces.BigSegmentStoreMetadata, error) {
	c := store.getConn()
	defer c.Close() //nolint:errcheck

	valueStr, err := r.String(c.Do("GET", bigSegmentsSyncTimeKey(store.prefix)))
	if err != nil {
		return interfaces.BigSegmentStoreMetadata{}, err
	}
	value, err := strconv.ParseUint(valueStr, 10, 64)
	if err != nil {
		return interfaces.BigSegmentStoreMetadata{}, err
	}

	return interfaces.BigSegmentStoreMetadata{
		LastUpToDate: ldtime.UnixMillisecondTime(value),
	}, nil
}

func (store *redisBigSegmentStoreImpl) GetMembership(
	contextHashKey string,
) (interfaces.BigSegmentMembership, error) {
	c := store.getConn()
	defer c.Close() //nolint:errcheck

	includedRefs, err := r.Strings(c.Do("SMEMBERS", bigSegmentsIncludeKey(store.prefix, contextHashKey)))
	if err != nil {
		return nil, err
	}
	excludedRefs, err := r.Strings(c.Do("SMEMBERS", bigSegmentsExcludeKey(store.prefix, contextHashKey)))
	if err != nil {
		return nil, err
	}

	return ldstoreimpl.NewBigSegmentMembershipFromSegmentRefs(includedRefs, excludedRefs), nil
}

func (store *redisBigSegmentStoreImpl) Close() error {
	return store.pool.Close()
}

func (store *redisBigSegmentStoreImpl) getConn() r.Conn {
	return store.pool.Get()
}

func bigSegmentsSyncTimeKey(prefix string) string {
	return fmt.Sprintf("%s:big_segments_synchronized_on", prefix)
}

func bigSegmentsIncludeKey(prefix, contextHashKey string) string {
	return fmt.Sprintf("%s:big_segment_include:%s", prefix, contextHashKey)
}

func bigSegmentsExcludeKey(prefix, contextHashKey string) string {
	return fmt.Sprintf("%s:big_segment_exclude:%s", prefix, contextHashKey)
}
