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
	upsertMode UpsertMode
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

// What the availability probe runs the script against in [UpsertModeAtomicScript]. The key holds no
// data and the probe never creates it, because the script writes only when the field already holds
// the value the caller expected, and no value the store stores can equal availabilityProbeValue.
const (
	probeKey               = "$availability"
	availabilityProbeField = "probe"
	availabilityProbeValue = "$not-a-stored-value"
)

// The maximum number of attempts Upsert will make before giving up. If the attempts run out, the
// item was not written and an error is returned.
//
// What forces a retry depends on the mode. [UpsertModeAtomicScript] retries only when another client
// changes the same item during the attempt, and the version check settles that case first, so the
// limit is not reached in practice. [UpsertModeWatch] watches the entire hash for a data kind rather
// than the individual item key, so every concurrent update of that kind contends on the same key,
// and a burst of updates to different items can exhaust the limit. Refer to [UpsertModeWatch] for
// what the SDK does with the error. Without a limit a caller could be starved indefinitely during
// such a burst. This matches the limit used by the go-redis implementation of this store.
const maxRetries = 10

// The values Upsert passes to the script in ARGV[2] to say whether it read a value for the item.
// These are explicit strings rather than a Go bool because the script compares against the string
// literal '1', and how a bool reaches the wire is up to the Redis client.
const (
	upsertExpectExisting = "1"
	upsertExpectAbsent   = "0"
)

// The statuses the script returns in the first element of its reply.
const (
	upsertStatusOK   = "OK"   // the item was written
	upsertStatusNoop = "NOOP" // the item had changed since Upsert read it, so nothing was written
)

// upsertScript replaces one field of a Redis hash, but only if the field still holds the value that
// Upsert read. Comparing the raw value keeps the script independent of how the SDK serializes an
// item: the format is not guaranteed to be JSON, and does not have to expose the version anywhere
// the script could read it.
//
// ARGV[2] is upsertExpectExisting when Upsert read a value for the item, and upsertExpectAbsent when
// it read none. When the script refuses to write, it returns the value it found, so the retry does
// not need to read the item again.
//
// If the server has forgotten the script, redigo recovers by matching the "NOSCRIPT " prefix of the
// server error and resending the script body with EVAL. A Redis-protocol proxy that rewords that
// error defeats the fallback, and Upsert then fails with the reworded error.
var upsertScript = r.NewScript(1, `
local current = redis.call('HGET', KEYS[1], ARGV[1])
local expected = false
if ARGV[2] == '1' then
    expected = ARGV[3]
end
if current ~= expected then
    if current == false then
        return {'NOOP'}
    end
    return {'NOOP', current}
end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[4])
return {'OK'}
`)

func newRedisDataStoreImpl(
	builder builderOptions,
	loggers ldlog.Loggers,
) *redisDataStoreImpl {
	impl := &redisDataStoreImpl{
		prefix:     builder.prefix,
		pool:       builder.pool,
		upsertMode: builder.upsertMode,
		loggers:    loggers,
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

// Upsert writes the item unless the store already holds that item at the same or a newer version.
// Reading the current version and writing the new one have to be atomic with respect to other
// clients; how that is achieved depends on the configured mode. See [UpsertMode].
func (store *redisDataStoreImpl) Upsert(
	kind ldstoretypes.DataKind,
	key string,
	newItem ldstoretypes.SerializedItemDescriptor,
) (bool, error) {
	if store.upsertMode == UpsertModeWatch {
		return store.upsertWithWatch(kind, key, newItem)
	}
	return store.upsertWithScript(kind, key, newItem)
}

// upsertWithScript retries the update as long as another client keeps changing the same item, up to
// maxRetries attempts. Every refusal carries the value the script found, so only the first attempt
// has to read the item.
func (store *redisDataStoreImpl) upsertWithScript(
	kind ldstoretypes.DataKind,
	key string,
	newItem ldstoretypes.SerializedItemDescriptor,
) (bool, error) {
	var known *ldstoretypes.SerializedItemDescriptor
	for range maxRetries {
		updated, refused, err := store.tryUpsertWithScript(kind, key, newItem, known)
		if err != nil || refused == nil {
			return updated, err
		}
		known = refused
	}
	return false, upsertRetriesExhaustedError(kind, key)
}

// tryUpsertWithScript makes a single attempt at the compare-and-set, acquiring its own connection
// from the pool and returning it to the pool before the attempt ends. If known is nil, the attempt
// starts by reading the item; otherwise it compares against the value it was given, which is what
// makes a retry cost one round trip instead of two.
//
// A non-nil refused is the value the script found when it declined to write, and means the caller
// should try again with that value; updated and err are not meaningful in that case.
func (store *redisDataStoreImpl) tryUpsertWithScript(
	kind ldstoretypes.DataKind,
	key string,
	newItem ldstoretypes.SerializedItemDescriptor,
	known *ldstoretypes.SerializedItemDescriptor,
) (updated bool, refused *ldstoretypes.SerializedItemDescriptor, err error) {
	c := store.getConn()
	defer c.Close() // nolint:errcheck

	var oldItem ldstoretypes.SerializedItemDescriptor
	if known == nil {
		oldItem, err = store.getWithConn(c, kind, key)
		if err != nil {
			return false, nil, err
		}
	} else {
		oldItem = *known
	}

	if !store.isNewerVersion(kind, key, oldItem, newItem) {
		return false, nil, nil
	}

	if store.testTxHook != nil { // instrumentation for unit tests
		store.testTxHook()
	}

	updated, observed, err := store.runUpsertScript(c, kind, key, oldItem, newItem)
	if err != nil {
		return false, nil, err
	}
	if updated {
		return true, nil, nil
	}

	if store.loggers.IsDebugEnabled() {
		store.loggers.Debug("Concurrent modification of the same item detected, retrying")
	}
	return false, &observed, nil
}

// runUpsertScript runs the compare-and-set. When the script declines to write, observed is the value
// the item held at that moment, which the next attempt compares against instead of reading again.
func (store *redisDataStoreImpl) runUpsertScript(
	c r.Conn,
	kind ldstoretypes.DataKind,
	key string,
	oldItem ldstoretypes.SerializedItemDescriptor,
	newItem ldstoretypes.SerializedItemDescriptor,
) (updated bool, observed ldstoretypes.SerializedItemDescriptor, err error) {
	expected := upsertExpectAbsent
	if oldItem.SerializedItem != nil {
		expected = upsertExpectExisting
	}

	reply, err := r.Values(upsertScript.Do(c, store.featuresKey(kind), key,
		expected, oldItem.SerializedItem, newItem.SerializedItem))
	if err != nil {
		return false, observed, err
	}
	if len(reply) == 0 {
		return false, observed, fmt.Errorf("upsert of key %q in %q got an empty reply from Redis",
			key, kind.GetName())
	}

	status, err := r.String(reply[0], nil)
	if err != nil {
		return false, observed, fmt.Errorf("upsert of key %q in %q got an unreadable status from Redis: %w",
			key, kind.GetName(), err)
	}

	switch status {
	case upsertStatusOK:
		return true, observed, nil
	case upsertStatusNoop:
		if len(reply) < 2 || reply[1] == nil {
			// The item no longer exists, so the next attempt should create it.
			return false, ldstoretypes.SerializedItemDescriptor{}.NotFound(), nil
		}
		value, err := r.Bytes(reply[1], nil)
		if err != nil {
			return false, observed, fmt.Errorf("upsert of key %q in %q got an unreadable value from Redis: %w",
				key, kind.GetName(), err)
		}
		return false, ldstoretypes.SerializedItemDescriptor{Version: 0, SerializedItem: value}, nil
	default:
		return false, observed, fmt.Errorf("upsert of key %q in %q got unexpected status %q from Redis",
			key, kind.GetName(), status)
	}
}

// upsertWithWatch retries the update as long as another client keeps modifying the watched key, up
// to maxRetries attempts.
func (store *redisDataStoreImpl) upsertWithWatch(
	kind ldstoretypes.DataKind,
	key string,
	newItem ldstoretypes.SerializedItemDescriptor,
) (bool, error) {
	baseKey := store.featuresKey(kind)
	for range maxRetries {
		updated, retry, err := store.tryUpsertWithWatch(kind, key, baseKey, newItem)
		if !retry {
			return updated, err
		}
	}
	return false, upsertRetriesExhaustedError(kind, key)
}

// tryUpsertWithWatch makes a single attempt at the optimistic-concurrency update, acquiring its own
// connection from the pool and returning it to the pool before the attempt ends. If retry is
// true, the watched key was modified by another client before the transaction committed and the
// caller should try again; updated and err are not meaningful in that case.
func (store *redisDataStoreImpl) tryUpsertWithWatch(
	kind ldstoretypes.DataKind,
	key string,
	baseKey string,
	newItem ldstoretypes.SerializedItemDescriptor,
) (updated bool, retry bool, err error) {
	c := store.getConn()
	defer c.Close() // nolint:errcheck

	_, err = c.Do("WATCH", baseKey)
	if err != nil {
		return false, false, err
	}

	defer c.Send("UNWATCH") // nolint:errcheck // this should always succeed

	if store.testTxHook != nil { // instrumentation for unit tests
		store.testTxHook()
	}

	oldItem, err := store.getWithConn(c, kind, key)
	if err != nil {
		return false, false, err
	}

	if !store.isNewerVersion(kind, key, oldItem, newItem) {
		return false, false, nil
	}

	_ = c.Send("MULTI")
	err = c.Send("HSET", baseKey, key, newItem.SerializedItem)
	if err == nil {
		var result interface{}
		result, err = c.Do("EXEC")
		if err != nil {
			return false, false, err
		}
		if result == nil {
			// if exec returned nothing, it means the watch was triggered and we should retry
			if store.loggers.IsDebugEnabled() {
				store.loggers.Debug("Concurrent modification detected, retrying")
			}
			return false, true, nil
		}
		return true, false, nil
	}
	return false, false, err
}

// isNewerVersion reports whether newItem should replace what the store currently holds. In this
// implementation the version is part of the serialized item, so finding the current version means
// parsing the value that was read.
func (store *redisDataStoreImpl) isNewerVersion(
	kind ldstoretypes.DataKind,
	key string,
	oldItem ldstoretypes.SerializedItemDescriptor,
	newItem ldstoretypes.SerializedItemDescriptor,
) bool {
	oldVersion := oldItem.Version
	if oldItem.SerializedItem != nil {
		parsed, _ := kind.Deserialize(oldItem.SerializedItem)
		oldVersion = parsed.Version
	}
	if oldVersion < newItem.Version {
		return true
	}

	if store.loggers.IsDebugEnabled() {
		updateOrDelete := "update"
		if newItem.Deleted {
			updateOrDelete = "delete"
		}
		store.loggers.Debugf(`Attempted to %s key: %s version: %d in "%s" with a version that is the same or older: %d`,
			updateOrDelete, key, oldVersion, kind, newItem.Version)
	}
	return false
}

func upsertRetriesExhaustedError(kind ldstoretypes.DataKind, key string) error {
	return fmt.Errorf("failed to update key %q in %q after %d attempts",
		key, kind.GetName(), maxRetries)
}

func (store *redisDataStoreImpl) IsInitialized() bool {
	c := store.getConn()
	defer c.Close() // nolint:errcheck
	inited, _ := r.Bool(c.Do("EXISTS", store.initedKey()))
	return inited
}

// IsStoreAvailable reports whether the store can serve the operations the SDK needs from it. The SDK
// calls this after an operation has failed, and treats the store as recovered as soon as it returns
// true.
//
// The probe therefore has to exercise what Upsert needs. In [UpsertModeAtomicScript] that includes
// permission to run the script, which a command such as EXISTS does not require: a server that
// refuses scripting answers EXISTS happily while every Upsert fails, so the SDK would declare
// recovery a few hundred milliseconds after each failure, restart the data source, write the whole
// environment again, and fail again, without ever settling. Running the script instead makes the
// store report itself unavailable and stay that way until scripting works.
func (store *redisDataStoreImpl) IsStoreAvailable() bool {
	c := store.getConn()
	defer c.Close() // nolint:errcheck

	if store.upsertMode == UpsertModeWatch {
		_, err := r.Bool(c.Do("EXISTS", store.initedKey()))
		return err == nil
	}

	// The script cannot write anything here: the expected value does not match what the field holds,
	// and if it somehow did, the replacement is the same value.
	reply, err := r.Values(upsertScript.Do(c, store.probeKey(), availabilityProbeField,
		upsertExpectExisting, availabilityProbeValue, availabilityProbeValue))
	return err == nil && len(reply) > 0
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

func (store *redisDataStoreImpl) probeKey() string {
	return store.prefix + ":" + probeKey
}

func (store *redisDataStoreImpl) getConn() r.Conn {
	return store.pool.Get()
}
