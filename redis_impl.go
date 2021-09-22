package ldredis

import (
	"net/url"
	"time"

	r "github.com/gomodule/redigo/redis"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldlog"
	"gopkg.in/launchdarkly/go-server-sdk.v5/interfaces/ldstoretypes"
)

// Internal implementation of the PersistentDataStore interface for Redis.
type redisDataStoreImpl struct {
	prefix     string
	pool       Pool
	loggers    ldlog.Loggers
	testTxHook func()
}

func newPool(url string, dialOptions []r.DialOption) *r.Pool {
	pool := &r.Pool{
		MaxIdle:     20,
		MaxActive:   16,
		Wait:        true,
		IdleTimeout: 300 * time.Second,
		Dial: func() (c r.Conn, err error) {
			c, err = r.DialURL(url, dialOptions...)
			return
		},
		TestOnBorrow: func(c r.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
	return pool
}

const initedKey = "$inited"

func newRedisDataStoreImpl(
	builder *DataStoreBuilder,
	loggers ldlog.Loggers,
) *redisDataStoreImpl {
	impl := &redisDataStoreImpl{
		prefix:  builder.prefix,
		pool:    builder.pool,
		loggers: loggers,
	}
	impl.loggers.SetPrefix("RedisDataStore:")

	if impl.pool == nil {
		logRedisURL(loggers, builder.url)
		impl.pool = newPool(builder.url, builder.dialOptions)
	}
	return impl
}

func logRedisURL(loggers ldlog.Loggers, redisURL string) {
	if parsed, err := url.Parse(redisURL); err == nil {
		loggers.Infof("Using URL: %s", urlToRedactedString(parsed))
	} else {
		loggers.Errorf("Invalid Redis URL: %s", redisURL) // we can assume that the Redis client will also fail
	}
}

// Equivalent to URL.Redacted() in Go 1.15+; currently we still support Go 1.14
func urlToRedactedString(parsed *url.URL) string {
	if parsed != nil && parsed.User != nil {
		if _, hasPW := parsed.User.Password(); hasPW {
			transformed := *parsed
			transformed.User = url.UserPassword(parsed.User.Username(), "xxxxx")
			return transformed.String()
		}
	}
	return parsed.String()
}

func (store *redisDataStoreImpl) Init(allData []ldstoretypes.SerializedCollection) error {
	c := store.getConn()
	defer c.Close() // nolint:errcheck

	_ = c.Send("MULTI")

	totalCount := 0

	for _, coll := range allData {
		baseKey := store.featuresKey(coll.Kind)

		_ = c.Send("DEL", baseKey)

		totalCount += len(coll.Items)
		for _, keyedItem := range coll.Items {
			_ = c.Send("HSET", baseKey, keyedItem.Key, keyedItem.Item.SerializedItem)
		}
	}

	_ = c.Send("SET", store.initedKey(), "")

	_, err := c.Do("EXEC")

	if err == nil {
		store.loggers.Infof("Initialized with %d items", totalCount)
	}

	return err
}

func (store *redisDataStoreImpl) Get(
	kind ldstoretypes.DataKind,
	key string,
) (ldstoretypes.SerializedItemDescriptor, error) {
	c := store.getConn()
	defer c.Close() // nolint:errcheck

	jsonStr, err := r.String(c.Do("HGET", store.featuresKey(kind), key))

	if err != nil {
		if err == r.ErrNil {
			if store.loggers.IsDebugEnabled() { // COVERAGE: tests don't verify debug logging
				store.loggers.Debugf("Key: %s not found in \"%s\"", key, kind.GetName())
			}
			return ldstoretypes.SerializedItemDescriptor{}.NotFound(), nil
		}
		return ldstoretypes.SerializedItemDescriptor{}.NotFound(), err
	}

	return ldstoretypes.SerializedItemDescriptor{Version: 0, SerializedItem: []byte(jsonStr)}, nil
}

func (store *redisDataStoreImpl) GetAll(
	kind ldstoretypes.DataKind,
) ([]ldstoretypes.KeyedSerializedItemDescriptor, error) {
	c := store.getConn()
	defer c.Close() // nolint:errcheck

	values, err := r.StringMap(c.Do("HGETALL", store.featuresKey(kind)))

	if err != nil && err != r.ErrNil {
		return nil, err
	}

	results := make([]ldstoretypes.KeyedSerializedItemDescriptor, 0, len(values))
	for k, v := range values {
		results = append(results, ldstoretypes.KeyedSerializedItemDescriptor{
			Key:  k,
			Item: ldstoretypes.SerializedItemDescriptor{Version: 0, SerializedItem: []byte(v)},
		})
	}
	return results, nil
}

func (store *redisDataStoreImpl) Upsert(
	kind ldstoretypes.DataKind,
	key string,
	newItem ldstoretypes.SerializedItemDescriptor,
) (bool, error) {
	baseKey := store.featuresKey(kind)
	for {
		// We accept that we can acquire multiple connections here and defer inside loop but we don't expect many
		c := store.getConn()
		defer c.Close() // nolint:errcheck

		_, err := c.Do("WATCH", baseKey)
		if err != nil {
			return false, err
		}

		defer c.Send("UNWATCH") // nolint:errcheck // this should always succeed

		if store.testTxHook != nil { // instrumentation for unit tests
			store.testTxHook()
		}

		oldItem, err := store.Get(kind, key)
		if err != nil { // COVERAGE: can't cause an error here in unit tests
			return false, err
		}

		// In this implementation, we have to parse the existing item in order to determine its version.
		oldVersion := oldItem.Version
		if oldItem.SerializedItem != nil {
			parsed, _ := kind.Deserialize(oldItem.SerializedItem)
			oldVersion = parsed.Version
		}

		if oldVersion >= newItem.Version {
			updateOrDelete := "update"
			if newItem.Deleted {
				updateOrDelete = "delete"
			}
			if store.loggers.IsDebugEnabled() { // COVERAGE: tests don't verify debug logging
				store.loggers.Debugf(`Attempted to %s key: %s version: %d in "%s" with a version that is the same or older: %d`,
					updateOrDelete, key, oldVersion, kind, newItem.Version)
			}
			return false, nil
		}

		_ = c.Send("MULTI")
		err = c.Send("HSET", baseKey, key, newItem.SerializedItem)
		if err == nil {
			var result interface{}
			result, err = c.Do("EXEC")
			if err == nil {
				if result == nil {
					// if exec returned nothing, it means the watch was triggered and we should retry
					if store.loggers.IsDebugEnabled() { // COVERAGE: tests don't verify debug logging
						store.loggers.Debug("Concurrent modification detected, retrying")
					}
					continue
				}
			}
			return true, nil
		}
		return false, err // COVERAGE: can't cause an error here in unit tests
	}
}

func (store *redisDataStoreImpl) IsInitialized() bool {
	c := store.getConn()
	defer c.Close() // nolint:errcheck
	inited, _ := r.Bool(c.Do("EXISTS", store.initedKey()))
	return inited
}

func (store *redisDataStoreImpl) IsStoreAvailable() bool {
	c := store.getConn()
	defer c.Close() // nolint:errcheck
	_, err := r.Bool(c.Do("EXISTS", store.initedKey()))
	return err == nil
}

func (store *redisDataStoreImpl) Close() error {
	return store.pool.Close()
}

func (store *redisDataStoreImpl) featuresKey(kind ldstoretypes.DataKind) string {
	return store.prefix + ":" + kind.GetName()
}

func (store *redisDataStoreImpl) initedKey() string {
	return store.prefix + ":" + initedKey
}

func (store *redisDataStoreImpl) getConn() r.Conn {
	return store.pool.Get()
}
