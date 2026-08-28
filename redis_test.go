package ldredis

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
	"github.com/launchdarkly/go-server-sdk/v7/testhelpers/storetest"
)

const redisURL = "redis://localhost:6379"

// Every mode has to satisfy the whole store contract, so the tests that are not about a specific
// mode run once for each of them.
var upsertModes = []struct {
	name string
	mode UpsertMode
}{
	{name: "atomic script", mode: UpsertModeAtomicScript},
	{name: "watch", mode: UpsertModeWatch},
}

func TestRedisDataStore(t *testing.T) {
	for _, m := range upsertModes {
		t.Run(m.name, func(t *testing.T) {
			storetest.NewPersistentDataStoreTestSuite(makeTestStore(m.mode), clearTestData).
				ErrorStoreFactory(makeFailedStore(m.mode), verifyFailedStoreError).
				ConcurrentModificationHook(setConcurrentModificationHook).
				Run(t)
		})
	}
}

func makeTestStore(mode UpsertMode) func(string) subsystems.ComponentConfigurer[subsystems.PersistentDataStore] {
	return func(prefix string) subsystems.ComponentConfigurer[subsystems.PersistentDataStore] {
		return DataStore().Prefix(prefix).UpsertMode(mode)
	}
}

func makeFailedStore(mode UpsertMode) subsystems.ComponentConfigurer[subsystems.PersistentDataStore] {
	// Here we ensure that all Redis operations will fail by using an invalid hostname.
	return DataStore().URL("redis://not-a-real-host").UpsertMode(mode)
}

// newTestStore builds a store that talks to the real Redis server, for the tests that need to
// interleave operations from more than one client or to inspect the keys themselves.
func newTestStore(t *testing.T, prefix string, mode UpsertMode) *redisDataStoreImpl {
	t.Helper()
	store := newRedisDataStoreImpl(
		builderOptions{prefix: prefix, url: redisURL, upsertMode: mode},
		ldlog.NewDisabledLoggers(),
	)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// serializedFlag builds an item of about valueSize bytes. The size matters because both modes send
// the item over the wire, and the script sends the value it expects to find as well.
func serializedFlag(key string, version int, valueSize int) ldstoretypes.SerializedItemDescriptor {
	paddingSize := max(0, valueSize-len(key)-64)
	serialized := fmt.Appendf(nil, `{"key":%q,"version":%d,"padding":"%0*s"}`,
		key, version, paddingSize, "")
	return ldstoretypes.SerializedItemDescriptor{Version: version, SerializedItem: serialized}
}

func verifyFailedStoreError(t assert.TestingT, err error) {
	assert.Contains(t, err.Error(), "lookup")
}

func clearTestData(prefix string) error {
	if prefix == "" {
		prefix = DefaultPrefix
	}

	client, err := r.DialURL(redisURL)
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck // test cleanup

	cursor := 0
	for {
		resp, err := client.Do("SCAN", fmt.Sprintf("%d", cursor), "MATCH", prefix+":*")
		if err != nil {
			return err
		}
		respValue, err := parseRedisResponseAsValue(resp)
		badResponse := func() error {
			return fmt.Errorf("unexpected format of Redis response: %s", respValue)
		}
		if err != nil {
			return err
		}
		if respValue.Count() != 2 {
			return badResponse()
		}
		cursor, err = strconv.Atoi(respValue.GetByIndex(0).StringValue())
		if err != nil {
			return badResponse()
		}
		respLines := respValue.GetByIndex(1)
		if respLines.Type() != ldvalue.ArrayType {
			return badResponse()
		}
		var failure error
		for i := 0; i < respLines.Count(); i++ {
			value := respLines.GetByIndex(i)
			redisKey := strings.TrimPrefix(strings.TrimSuffix(value.String(), `"`), `"`)
			failure = client.Send("DEL", redisKey)
			if failure != nil {
				break
			}
		}
		if failure != nil {
			return failure
		}
		if cursor == 0 { // SCAN returns 0 when the current result subset is the last one
			break
		}
	}
	return client.Flush()
}

func setConcurrentModificationHook(store subsystems.PersistentDataStore, hook func()) {
	store.(*redisDataStoreImpl).testTxHook = hook
}

func parseRedisResponseAsValue(resp interface{}) (ldvalue.Value, error) {
	switch t := resp.(type) {
	case []interface{}:
		a := ldvalue.ArrayBuild()
		for _, item := range t {
			v, err := parseRedisResponseAsValue(item)
			if err != nil {
				return ldvalue.Null(), err
			}
			a.Add(v)
		}
		return a.Build(), nil
	case []byte:
		return ldvalue.String(string(t)), nil
	default:
		return ldvalue.Null(), fmt.Errorf("unexpected data type in response: %T", resp)
	}
}
