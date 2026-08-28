package ldredis

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
)

func TestScriptModeAvailabilityProbeRunsTheScript(t *testing.T) {
	tests := []struct {
		name      string
		conn      *upsertTestConn
		available bool
	}{
		{
			name:      "the script runs",
			conn:      &upsertTestConn{evalReply: []interface{}{[]byte(upsertStatusNoop)}},
			available: true,
		},
		{
			name:      "the script fails",
			conn:      &upsertTestConn{evalErr: errors.New("NOPERM no permission to run the 'evalsha' command")},
			available: false,
		},
		{
			name:      "the reply is unusable",
			conn:      &upsertTestConn{evalReply: []interface{}{}},
			available: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{test.conn}}
			store := newUpsertTestStore(pool, UpsertModeAtomicScript, ldlog.NewDisabledLoggers())

			assert.Equal(t, test.available, store.IsStoreAvailable())
			assert.Equal(t, []string{"EVALSHA"}, test.conn.commands,
				"a probe that does not run the script cannot see that scripting is denied")
			assert.Equal(t, 1, test.conn.closeCount)
			assert.Zero(t, pool.activeCount)
		})
	}
}

func TestWatchModeAvailabilityProbeUsesExists(t *testing.T) {
	tests := []struct {
		name      string
		conn      *upsertTestConn
		available bool
	}{
		{
			name:      "the command runs",
			conn:      &upsertTestConn{existsReply: int64(0)},
			available: true,
		},
		{
			name:      "the command fails",
			conn:      &upsertTestConn{existsErr: errors.New("EXISTS failed")},
			available: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{test.conn}}
			store := newUpsertTestStore(pool, UpsertModeWatch, ldlog.NewDisabledLoggers())

			assert.Equal(t, test.available, store.IsStoreAvailable())
			assert.Equal(t, []string{"EXISTS"}, test.conn.commands,
				"this mode needs no scripting, so the probe should not require it either")
			assert.Equal(t, 1, test.conn.closeCount)
			assert.Zero(t, pool.activeCount)
		})
	}
}

func TestScriptModeAvailabilityProbeWritesNothing(t *testing.T) {
	const prefix = "test-availability"
	require.NoError(t, clearTestData(prefix))
	t.Cleanup(func() { assert.NoError(t, clearTestData(prefix)) })

	store := newRedisDataStoreImpl(
		builderOptions{prefix: prefix, url: redisURL, upsertMode: UpsertModeAtomicScript},
		ldlog.NewDisabledLoggers(),
	)
	defer store.Close() //nolint:errcheck // test cleanup

	require.True(t, store.IsStoreAvailable())

	c := store.getConn()
	defer c.Close() //nolint:errcheck // test cleanup
	exists, err := r.Int(c.Do("EXISTS", store.probeKey()))
	require.NoError(t, err)
	assert.Equal(t, 0, exists, "the probe must not create the key it probes")
}

// The SDK polls IsStoreAvailable after a failure and treats the store as recovered as soon as the
// probe says it is. A probe that reports a scripting-denied store healthy therefore makes the SDK
// declare recovery, refresh the whole store, fail again, and repeat for as long as the process runs.
func TestStoreWithoutScriptingPermission(t *testing.T) {
	const username = "ldredis-test-no-scripting"
	const prefix = "test-no-scripting"

	admin, err := r.DialURL(redisURL)
	require.NoError(t, err)
	// Registered first so that it runs after the cleanup that still needs this connection.
	t.Cleanup(func() { _ = admin.Close() })

	if _, err := admin.Do("ACL", "SETUSER", username, "on", ">pw", "~*", "+@all", "-@scripting"); err != nil {
		t.Skipf("this Redis server does not support ACL users: %v", err)
	}
	t.Cleanup(func() {
		_, err := admin.Do("ACL", "DELUSER", username)
		assert.NoError(t, err, "the test user must not be left on the server")
	})
	require.NoError(t, clearTestData(prefix))
	t.Cleanup(func() { assert.NoError(t, clearTestData(prefix)) })

	url := strings.Replace(redisURL, "redis://", fmt.Sprintf("redis://%s:pw@", username), 1)
	newItem := ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte(`{"key":"flag-key","version":1}`),
	}

	newStore := func(mode UpsertMode) *redisDataStoreImpl {
		store := newRedisDataStoreImpl(
			builderOptions{prefix: prefix, url: url, upsertMode: mode},
			ldlog.NewDisabledLoggers(),
		)
		t.Cleanup(func() { _ = store.Close() })
		return store
	}

	t.Run("the script mode reports itself unavailable", func(t *testing.T) {
		store := newStore(UpsertModeAtomicScript)

		updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", newItem)
		require.Error(t, err, "this mode needs permission to run scripts")
		assert.False(t, updated)

		assert.False(t, store.IsStoreAvailable(),
			"the store must stay unavailable while the only thing it needs is denied")
	})

	t.Run("the watch mode still works", func(t *testing.T) {
		store := newStore(UpsertModeWatch)

		updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", newItem)
		require.NoError(t, err, "this mode needs no scripting")
		assert.True(t, updated)

		assert.True(t, store.IsStoreAvailable())
	})
}
