package ldredis

import (
	"errors"
	"fmt"
	"testing"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
)

func TestUpsertReturnsExecError(t *testing.T) {
	execErr := errors.New("EXEC failed")
	conn := &execErrorConn{execErr: execErr}
	pool := &scriptedConnectionPool{t: t, connections: []*execErrorConn{conn}}
	store := &redisDataStoreImpl{
		prefix:  "test",
		pool:    pool,
		loggers: ldlog.NewDisabledLoggers(),
	}

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte(`{"key":"flag-key","version":1}`),
	})

	require.False(t, updated, "a failed transaction must not be reported as an update")
	require.ErrorIs(t, err, execErr)
	require.Equal(t, 1, pool.getCount)
	require.Equal(t, 1, conn.closeCount, "the connection should be returned after EXEC fails")
}

func TestUpsertReturnsConnectionOnCommandErrors(t *testing.T) {
	watchErr := errors.New("WATCH failed")
	hgetErr := errors.New("HGET failed")
	hsetErr := errors.New("HSET failed")

	tests := []struct {
		name string
		conn *execErrorConn
		err  error
	}{
		{name: "watch", conn: &execErrorConn{watchErr: watchErr}, err: watchErr},
		{name: "read", conn: &execErrorConn{hgetErr: hgetErr}, err: hgetErr},
		{name: "write", conn: &execErrorConn{hsetErr: hsetErr}, err: hsetErr},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := &scriptedConnectionPool{t: t, connections: []*execErrorConn{test.conn}}
			store := &redisDataStoreImpl{prefix: "test", pool: pool, loggers: ldlog.NewDisabledLoggers()}

			updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
				Version:        1,
				SerializedItem: []byte("new"),
			})

			require.False(t, updated)
			require.ErrorIs(t, err, test.err)
			require.Equal(t, 1, test.conn.closeCount)
		})
	}
}

func TestUpsertDoesNotReplaceNewerDeletedItem(t *testing.T) {
	conn := &execErrorConn{hgetReply: []byte("existing")}
	pool := &scriptedConnectionPool{t: t, connections: []*execErrorConn{conn}}
	loggers := ldlog.NewDisabledLoggers()
	loggers.SetMinLevel(ldlog.Debug)
	store := &redisDataStoreImpl{prefix: "test", pool: pool, loggers: loggers}

	updated, err := store.Upsert(execFixedVersionKind{version: 2}, "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		Deleted:        true,
		SerializedItem: []byte("new"),
	})

	require.NoError(t, err)
	require.False(t, updated)
	require.Equal(t, 1, conn.closeCount)
}

func TestUpsertRetriesWatchConflict(t *testing.T) {
	connections := []*execErrorConn{
		{execReply: nil},
		{execReply: []any{[]byte("OK")}},
	}
	pool := &scriptedConnectionPool{t: t, connections: connections}
	loggers := ldlog.NewDisabledLoggers()
	loggers.SetMinLevel(ldlog.Debug)
	store := &redisDataStoreImpl{prefix: "test", pool: pool, loggers: loggers}

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte("new"),
	})

	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, 2, pool.getCount)
	for _, conn := range connections {
		require.Equal(t, 1, conn.closeCount)
	}
}

type scriptedConnectionPool struct {
	t           *testing.T
	connections []*execErrorConn
	getCount    int
}

func (p *scriptedConnectionPool) Get() r.Conn {
	p.t.Helper()
	require.Less(p.t, p.getCount, len(p.connections), "unexpected extra connection request")
	conn := p.connections[p.getCount]
	p.getCount++
	return conn
}

func (p *scriptedConnectionPool) Close() error { return nil }

type execErrorConn struct {
	watchErr   error
	hgetReply  any
	hgetErr    error
	hsetErr    error
	execReply  any
	execErr    error
	closeCount int
}

func (c *execErrorConn) Close() error {
	c.closeCount++
	return nil
}

func (c *execErrorConn) Err() error { return nil }

func (c *execErrorConn) Do(commandName string, _ ...any) (any, error) {
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
	default:
		return nil, fmt.Errorf("unexpected Do command %q", commandName)
	}
}

func (c *execErrorConn) Send(commandName string, _ ...any) error {
	switch commandName {
	case "MULTI", "UNWATCH":
		return nil
	case "HSET":
		return c.hsetErr
	default:
		return fmt.Errorf("unexpected Send command %q", commandName)
	}
}

func (c *execErrorConn) Flush() error { return nil }

func (c *execErrorConn) Receive() (any, error) { return nil, nil }

type execFixedVersionKind struct{ version int }

func (k execFixedVersionKind) GetName() string { return "features" }

func (k execFixedVersionKind) Serialize(ldstoretypes.ItemDescriptor) []byte { return nil }

func (k execFixedVersionKind) Deserialize([]byte) (ldstoretypes.ItemDescriptor, error) {
	return ldstoretypes.ItemDescriptor{Version: k.version}, nil
}
