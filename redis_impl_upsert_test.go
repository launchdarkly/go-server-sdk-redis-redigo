package ldredis

import (
	"errors"
	"fmt"
	"testing"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-sdk-common/v3/ldlogtest"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
)

func TestWatchUpsertReleasesConnectionBeforeRetry(t *testing.T) {
	pool := &singleActiveConnectionPool{
		t: t,
		connections: []*upsertTestConn{
			{execReply: nil},
			{execReply: []interface{}{[]byte("OK")}},
		},
	}
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	store := newUpsertTestStore(pool, UpsertModeWatch, mockLog.Loggers)

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte(`{"key":"flag-key","version":1}`),
	})

	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, 2, pool.getCount, "the nil EXEC response should cause exactly one retry")
	require.Equal(t, 0, pool.activeCount, "all borrowed connections should be returned")
	require.Equal(t, 1, pool.maxActiveCount, "a retry must not retain the previous connection")
	for _, conn := range pool.connections {
		require.Equal(t, 1, conn.closeCount, "each connection should be closed exactly once")
	}
	mockLog.AssertMessageMatch(t, true, ldlog.Debug, "Concurrent modification detected, retrying")
}

func TestWatchUpsertReturnsConnectionOnErrors(t *testing.T) {
	watchErr := errors.New("WATCH failed")
	hgetErr := errors.New("HGET failed")
	hsetErr := errors.New("HSET failed")

	tests := []struct {
		name string
		conn *upsertTestConn
		err  error
	}{
		{name: "watch", conn: &upsertTestConn{watchErr: watchErr}, err: watchErr},
		{name: "read", conn: &upsertTestConn{hgetErr: hgetErr}, err: hgetErr},
		{name: "write", conn: &upsertTestConn{hsetErr: hsetErr}, err: hsetErr},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{test.conn}}
			store := newUpsertTestStore(pool, UpsertModeWatch, ldlog.NewDisabledLoggers())

			updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
				Version:        1,
				SerializedItem: []byte("new"),
			})

			require.False(t, updated)
			require.ErrorIs(t, err, test.err)
			require.Equal(t, 1, test.conn.closeCount)
			require.Zero(t, pool.activeCount)
		})
	}
}

func TestWatchUpsertDoesNotDeleteNewerItem(t *testing.T) {
	conn := &upsertTestConn{hgetReply: []byte("existing")}
	pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{conn}}
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	store := newUpsertTestStore(pool, UpsertModeWatch, mockLog.Loggers)

	updated, err := store.Upsert(fixedVersionKind{version: 2}, "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		Deleted:        true,
		SerializedItem: []byte("new"),
	})

	require.NoError(t, err)
	require.False(t, updated)
	require.Equal(t, 1, conn.closeCount)
	mockLog.AssertMessageMatch(t, true, ldlog.Debug,
		"Attempted to delete key: flag-key version: 2 .* with a version that is the same or older: 1")
}

func TestWatchUpsertReturnsExecError(t *testing.T) {
	execErr := errors.New("EXEC failed")
	conn := &upsertTestConn{execErr: execErr}
	pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{conn}}
	store := newUpsertTestStore(pool, UpsertModeWatch, ldlog.NewDisabledLoggers())

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte("new"),
	})

	require.False(t, updated, "a failed transaction must not be reported as an update")
	require.ErrorIs(t, err, execErr)
	require.Equal(t, 1, conn.closeCount, "the connection should be returned after EXEC fails")
	require.Zero(t, pool.activeCount)
}

func TestWatchUpsertGivesUpAfterMaxRetries(t *testing.T) {
	// Every attempt sees a nil EXEC reply, so the watched key looks perpetually contended. The pool
	// fails the test if Upsert asks for more connections than there are attempts allowed.
	connections := make([]*upsertTestConn, maxRetries)
	for i := range connections {
		connections[i] = &upsertTestConn{execReply: nil}
	}
	pool := &singleActiveConnectionPool{t: t, connections: connections}
	store := newUpsertTestStore(pool, UpsertModeWatch, ldlog.NewDisabledLoggers())

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte("new"),
	})

	require.False(t, updated, "an abandoned update must not be reported as an update")
	require.EqualError(t, err, `failed to update key "flag-key" in "features" after 10 attempts`)
	require.Equal(t, maxRetries, pool.getCount, "Upsert should make exactly maxRetries attempts")
	require.Zero(t, pool.activeCount)
	for _, conn := range connections {
		require.Equal(t, 1, conn.closeCount, "each connection should be closed exactly once")
	}
}

func TestScriptUpsertRetriesWithoutReadingTheItemAgain(t *testing.T) {
	pool := &singleActiveConnectionPool{
		t: t,
		connections: []*upsertTestConn{
			{
				hgetReply: []byte("value-the-attempt-read"),
				evalReply: []interface{}{[]byte(upsertStatusNoop), []byte("value-another-client-wrote")},
			},
			{evalReply: []interface{}{[]byte(upsertStatusOK)}},
		},
	}
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	store := newUpsertTestStore(pool, UpsertModeAtomicScript, mockLog.Loggers)

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte("new"),
	})

	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, []string{"HGET", "EVALSHA"}, pool.connections[0].commands)
	require.Equal(t, []string{"EVALSHA"}, pool.connections[1].commands,
		"a refused attempt reports the current value, so the retry must not read it again")
	require.Equal(t, []byte("value-another-client-wrote"), pool.connections[1].scriptArgs()[3],
		"the retry must compare against the value the script reported")
	require.Equal(t, 2, pool.getCount)
	require.Equal(t, 0, pool.activeCount, "all borrowed connections should be returned")
	require.Equal(t, 1, pool.maxActiveCount, "a retry must not retain the previous connection")
	for _, conn := range pool.connections {
		require.Equal(t, 1, conn.closeCount, "each connection should be closed exactly once")
	}
	mockLog.AssertMessageMatch(t, true, ldlog.Debug, "Concurrent modification of the same item detected, retrying")
}

func TestScriptUpsertComparesAgainstWhatItRead(t *testing.T) {
	tests := []struct {
		name          string
		hgetReply     interface{}
		expectFlag    string
		expectedValue []byte
	}{
		{
			name:          "item exists",
			hgetReply:     []byte("existing"),
			expectFlag:    upsertExpectExisting,
			expectedValue: []byte("existing"),
		},
		{
			name:          "item does not exist",
			expectFlag:    upsertExpectAbsent,
			expectedValue: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &upsertTestConn{
				hgetReply: test.hgetReply,
				evalReply: []interface{}{[]byte(upsertStatusOK)},
			}
			pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{conn}}
			store := newUpsertTestStore(pool, UpsertModeAtomicScript, ldlog.NewDisabledLoggers())

			updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
				Version:        1,
				SerializedItem: []byte("new"),
			})

			require.NoError(t, err)
			require.True(t, updated)

			args := conn.scriptArgs()
			require.Equal(t, "test:features", args[0])
			require.Equal(t, "flag-key", args[1])
			require.Equal(t, test.expectFlag, args[2],
				"the script decides between comparing a value and requiring absence from this argument")
			require.Equal(t, test.expectedValue, args[3])
			require.Equal(t, []byte("new"), args[4])
			require.Equal(t, 1, conn.closeCount)
		})
	}
}

func TestScriptUpsertRequiresAbsenceAfterTheItemDisappears(t *testing.T) {
	// A one-element NOOP reply means the item was gone when the script looked, so the retry has to
	// ask the script to create it rather than to match a value.
	pool := &singleActiveConnectionPool{
		t: t,
		connections: []*upsertTestConn{
			{hgetReply: []byte("existing"), evalReply: []interface{}{[]byte(upsertStatusNoop)}},
			{evalReply: []interface{}{[]byte(upsertStatusOK)}},
		},
	}
	store := newUpsertTestStore(pool, UpsertModeAtomicScript, ldlog.NewDisabledLoggers())

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte("new"),
	})

	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, upsertExpectAbsent, pool.connections[1].scriptArgs()[2])
}

func TestScriptUpsertDoesNotRunTheScriptForAnOlderVersion(t *testing.T) {
	conn := &upsertTestConn{hgetReply: []byte("existing")}
	pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{conn}}
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	store := newUpsertTestStore(pool, UpsertModeAtomicScript, mockLog.Loggers)

	updated, err := store.Upsert(fixedVersionKind{version: 2}, "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		Deleted:        true,
		SerializedItem: []byte("new"),
	})

	require.NoError(t, err)
	require.False(t, updated)
	require.Equal(t, []string{"HGET"}, conn.commands, "an older version must not reach the script")
	require.Equal(t, 1, conn.closeCount)
	mockLog.AssertMessageMatch(t, true, ldlog.Debug,
		"Attempted to delete key: flag-key version: 2 .* with a version that is the same or older: 1")
}

func TestScriptUpsertReturnsConnectionOnErrors(t *testing.T) {
	hgetErr := errors.New("HGET failed")
	evalErr := errors.New("EVALSHA failed")

	tests := []struct {
		name string
		conn *upsertTestConn
		err  error
	}{
		{name: "read", conn: &upsertTestConn{hgetErr: hgetErr}, err: hgetErr},
		{name: "script", conn: &upsertTestConn{evalErr: evalErr}, err: evalErr},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{test.conn}}
			store := newUpsertTestStore(pool, UpsertModeAtomicScript, ldlog.NewDisabledLoggers())

			updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
				Version:        1,
				SerializedItem: []byte("new"),
			})

			require.False(t, updated)
			require.ErrorIs(t, err, test.err)
			require.Equal(t, 1, test.conn.closeCount)
			require.Zero(t, pool.activeCount)
		})
	}
}

func TestScriptUpsertRejectsUnusableReplies(t *testing.T) {
	tests := []struct {
		name      string
		evalReply interface{}
		errorText string
	}{
		{
			name:      "not a list",
			evalReply: int64(1),
			errorText: "unexpected type",
		},
		{
			name:      "empty list",
			evalReply: []interface{}{},
			errorText: `upsert of key "flag-key" in "features" got an empty reply from Redis`,
		},
		{
			name:      "unreadable status",
			evalReply: []interface{}{int64(1)},
			errorText: `upsert of key "flag-key" in "features" got an unreadable status from Redis`,
		},
		{
			name:      "unknown status",
			evalReply: []interface{}{[]byte("MAYBE")},
			errorText: `upsert of key "flag-key" in "features" got unexpected status "MAYBE" from Redis`,
		},
		{
			name:      "unreadable value",
			evalReply: []interface{}{[]byte(upsertStatusNoop), int64(1)},
			errorText: `upsert of key "flag-key" in "features" got an unreadable value from Redis`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &upsertTestConn{evalReply: test.evalReply}
			pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{conn}}
			store := newUpsertTestStore(pool, UpsertModeAtomicScript, ldlog.NewDisabledLoggers())

			updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
				Version:        1,
				SerializedItem: []byte("new"),
			})

			require.False(t, updated)
			require.ErrorContains(t, err, test.errorText)
			require.Equal(t, 1, conn.closeCount)
			require.Zero(t, pool.activeCount)
		})
	}
}

func TestScriptUpsertGivesUpAfterMaxRetries(t *testing.T) {
	// Every attempt is refused, as if another client keeps rewriting the same item. The pool fails
	// the test if Upsert asks for more connections than there are attempts allowed.
	connections := make([]*upsertTestConn, maxRetries)
	for i := range connections {
		connections[i] = &upsertTestConn{
			evalReply: []interface{}{[]byte(upsertStatusNoop), []byte("changed-again")},
		}
	}
	pool := &singleActiveConnectionPool{t: t, connections: connections}
	store := newUpsertTestStore(pool, UpsertModeAtomicScript, ldlog.NewDisabledLoggers())

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte("new"),
	})

	require.False(t, updated, "an abandoned update must not be reported as an update")
	require.EqualError(t, err, `failed to update key "flag-key" in "features" after 10 attempts`)
	require.Equal(t, maxRetries, pool.getCount, "Upsert should make exactly maxRetries attempts")
	require.Zero(t, pool.activeCount)
	for _, conn := range connections {
		require.Equal(t, 1, conn.closeCount, "each connection should be closed exactly once")
	}
}

func newUpsertTestStore(pool Pool, mode UpsertMode, loggers ldlog.Loggers) *redisDataStoreImpl {
	return &redisDataStoreImpl{prefix: "test", pool: pool, upsertMode: mode, loggers: loggers}
}

type singleActiveConnectionPool struct {
	t              *testing.T
	connections    []*upsertTestConn
	getCount       int
	activeCount    int
	maxActiveCount int
}

func (p *singleActiveConnectionPool) Get() r.Conn {
	p.t.Helper()
	require.Less(p.t, p.getCount, len(p.connections), "unexpected extra connection request")
	require.Zero(p.t, p.activeCount, "Upsert requested another connection before closing the previous one")

	conn := p.connections[p.getCount]
	p.getCount++
	p.activeCount++
	if p.activeCount > p.maxActiveCount {
		p.maxActiveCount = p.activeCount
	}
	conn.pool = p
	return conn
}

func (p *singleActiveConnectionPool) Close() error { return nil }

type upsertTestConn struct {
	pool       *singleActiveConnectionPool
	commands   []string
	watchErr   error
	hgetReply  interface{}
	hgetErr    error
	hsetErr    error
	execReply  interface{}
	execErr    error
	evalReply  interface{}
	evalErr    error
	evalArgs   []interface{}
	closeCount int
}

func (c *upsertTestConn) Close() error {
	if c.closeCount == 0 {
		c.pool.activeCount--
	}
	c.closeCount++
	return nil
}

func (c *upsertTestConn) Err() error { return nil }

func (c *upsertTestConn) Do(commandName string, args ...interface{}) (interface{}, error) {
	c.commands = append(c.commands, commandName)
	switch commandName {
	case "WATCH":
		return "OK", c.watchErr
	case "HGET":
		if c.hgetReply == nil && c.hgetErr == nil {
			return nil, r.ErrNil
		}
		return c.hgetReply, c.hgetErr
	case "EXEC":
		return c.execReply, c.execErr
	case "EVALSHA":
		c.evalArgs = args
		return c.evalReply, c.evalErr
	default:
		return nil, fmt.Errorf("unexpected Do command %q", commandName)
	}
}

func (c *upsertTestConn) Send(commandName string, _ ...interface{}) error {
	c.commands = append(c.commands, commandName)
	switch commandName {
	case "MULTI", "UNWATCH":
		return nil
	case "HSET":
		return c.hsetErr
	default:
		return fmt.Errorf("unexpected Send command %q", commandName)
	}
}

func (c *upsertTestConn) Flush() error { return nil }

func (c *upsertTestConn) Receive() (interface{}, error) { return nil, nil }

// scriptArgs returns the KEYS and ARGV values of the script the connection was last asked to run,
// without the script hash and key count that the Redis client puts in front of them.
func (c *upsertTestConn) scriptArgs() []interface{} {
	return c.evalArgs[2:]
}

type fixedVersionKind struct{ version int }

func (k fixedVersionKind) GetName() string { return "features" }

func (k fixedVersionKind) Serialize(ldstoretypes.ItemDescriptor) []byte { return nil }

func (k fixedVersionKind) Deserialize([]byte) (ldstoretypes.ItemDescriptor, error) {
	return ldstoretypes.ItemDescriptor{Version: k.version}, nil
}
