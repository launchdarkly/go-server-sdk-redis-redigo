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

func TestUpsertReturnsConnectionOnErrors(t *testing.T) {
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
			store := &redisDataStoreImpl{prefix: "test", pool: pool, loggers: ldlog.NewDisabledLoggers()}

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

func TestUpsertDoesNotReplaceNewerDeletedItem(t *testing.T) {
	conn := &upsertTestConn{hgetReply: []byte("existing")}
	pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{conn}}
	loggers := ldlog.NewDisabledLoggers()
	loggers.SetMinLevel(ldlog.Debug)
	store := &redisDataStoreImpl{prefix: "test", pool: pool, loggers: loggers}

	updated, err := store.Upsert(fixedVersionKind{version: 2}, "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		Deleted:        true,
		SerializedItem: []byte("new"),
	})

	require.NoError(t, err)
	require.False(t, updated)
	require.Equal(t, 1, conn.closeCount)
}

func TestUpsertReturnsConnectionOnExecError(t *testing.T) {
	execErr := errors.New("EXEC failed")
	conn := &upsertTestConn{execErr: execErr}
	pool := &singleActiveConnectionPool{t: t, connections: []*upsertTestConn{conn}}
	store := &redisDataStoreImpl{prefix: "test", pool: pool, loggers: ldlog.NewDisabledLoggers()}

	_, _ = store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte("new"),
	})

	require.Equal(t, 1, conn.closeCount)
	require.Zero(t, pool.activeCount)
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
	watchErr   error
	hgetReply  any
	hgetErr    error
	hsetErr    error
	execReply  any
	execErr    error
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

func (c *upsertTestConn) Do(commandName string, _ ...any) (any, error) {
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

func (c *upsertTestConn) Send(commandName string, _ ...any) error {
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

func (c *upsertTestConn) Receive() (any, error) { return nil, nil }

type fixedVersionKind struct{ version int }

func (k fixedVersionKind) GetName() string { return "features" }

func (k fixedVersionKind) Serialize(ldstoretypes.ItemDescriptor) []byte { return nil }

func (k fixedVersionKind) Deserialize([]byte) (ldstoretypes.ItemDescriptor, error) {
	return ldstoretypes.ItemDescriptor{Version: k.version}, nil
}
