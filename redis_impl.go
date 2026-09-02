package ldredis

import (
	"fmt"
	"net/url"
	"time"

	r "github.com/gomodule/redigo/redis"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
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

// upsertScript atomically replaces one hash field only if it still has the value that Upsert read.
// Comparing the raw value keeps the script independent of the SDK's serialization format.
var upsertScript = r.NewScript(1, `
local current = redis.call('HGET', KEYS[1], ARGV[1])
if ARGV[2] == '1' then
    if current ~= ARGV[3] then
        return 0
    end
elseif current then
    return 0
end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[4])
return 1
`)

// The maximum number of attempts Upsert will make when another client concurrently changes the
// same item. Updates to other items in the same Redis hash do not cause retries.
const maxRetries = 10

func newRedisDataStoreImpl(
	builder builderOptions,
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
		loggers.Infof("Using URL: %s", parsed.Redacted())
	} else {
		loggers.Errorf("Invalid Redis URL: %s", redisURL) // we can assume that the Redis client will also fail
	}
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
	return store.getWithConn(c, kind, key)
}

func (store *redisDataStoreImpl) getWithConn(
	c r.Conn,
	kind ldstoretypes.DataKind,
	key string,
) (ldstoretypes.SerializedItemDescriptor, error) {
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
	for range maxRetries {
		updated, retry, err := store.tryUpsert(kind, key, newItem)
		if !retry {
			return updated, err
		}
	}
	return false, fmt.Errorf("failed to update key %q in %q after %d attempts",
		key, kind.GetName(), maxRetries)
}

func (store *redisDataStoreImpl) tryUpsert(
	kind ldstoretypes.DataKind,
	key string,
	newItem ldstoretypes.SerializedItemDescriptor,
) (updated bool, retry bool, err error) {
	c := store.getConn()
	defer c.Close() // nolint:errcheck

	oldItem, err := store.getWithConn(c, kind, key)
	if err != nil {
		return false, false, err
	}
	oldVersion := oldItem.Version
	oldExists := oldItem.SerializedItem != nil
	if oldExists {
		parsed, _ := kind.Deserialize(oldItem.SerializedItem)
		oldVersion = parsed.Version
	}
	if oldVersion >= newItem.Version {
		if store.loggers.IsDebugEnabled() {
			updateOrDelete := "update"
			if newItem.Deleted {
				updateOrDelete = "delete"
			}
			store.loggers.Debugf(`Attempted to %s key: %s version: %d in "%s" with a version that is the same or older: %d`,
				updateOrDelete, key, oldVersion, kind, newItem.Version)
		}
		return false, false, nil
	}

	if store.testTxHook != nil { // instrumentation for unit tests
		store.testTxHook()
	}

	updated, err = r.Bool(upsertScript.Do(c, store.featuresKey(kind), key,
		oldExists, oldItem.SerializedItem, newItem.SerializedItem))
	if err != nil {
		return false, false, err
	}
	if !updated {
		if store.loggers.IsDebugEnabled() {
			store.loggers.Debug("Concurrent modification of the same item detected, retrying")
		}
		return false, true, nil
	}
	return true, false, nil
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
