package ldredis

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v6/subsystems"
	"github.com/launchdarkly/go-server-sdk/v6/testhelpers/storetest"
)

const redisURL = "redis://localhost:6379"

func TestRedisDataStore(t *testing.T) {
	storetest.NewPersistentDataStoreTestSuite(makeTestStore, clearTestData).
		ErrorStoreFactory(makeFailedStore(), verifyFailedStoreError).
		ConcurrentModificationHook(setConcurrentModificationHook).
		Run(t)
}

func makeTestStore(prefix string) subsystems.ComponentConfigurer[subsystems.PersistentDataStore] {
	return DataStore().Prefix(prefix)
}

func makeFailedStore() subsystems.ComponentConfigurer[subsystems.PersistentDataStore] {
	// Here we ensure that all Redis operations will fail by using an invalid hostname.
	return DataStore().URL("redis://not-a-real-host")
}

func verifyFailedStoreError(t assert.TestingT, err error) {
	assert.Contains(t, err.Error(), "no such host")
}

func clearTestData(prefix string) error {
	if prefix == "" {
		prefix = DefaultPrefix
	}

	client, err := r.DialURL(redisURL)
	if err != nil {
		return err
	}
	defer client.Close()

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
