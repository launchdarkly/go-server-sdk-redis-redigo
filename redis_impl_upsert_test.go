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

func TestUpsertRunsAtomicScriptWithExistingStorageFormat(t *testing.T) {
	conn := &upsertTestConn{scriptReply: int64(1)}
	store := &redisDataStoreImpl{
		prefix:  "test",
		pool:    &upsertTestPool{conn: conn},
		loggers: ldlog.NewDisabledLoggers(),
	}
	item := ldstoretypes.SerializedItemDescriptor{
		Version:        2,
		SerializedItem: []byte(`{"key":"flag-key","version":2}`),
	}

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", item)

	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, "EVALSHA", conn.command)
	require.Len(t, conn.args, 7)
	require.Equal(t, 1, conn.args[1])
	require.Equal(t, "test:features", conn.args[2])
	require.Equal(t, "flag-key", conn.args[3])
	require.Equal(t, false, conn.args[4])
	require.Nil(t, conn.args[5])
	require.Equal(t, item.SerializedItem, conn.args[6])
	require.Equal(t, 1, conn.closeCount)
}

func TestUpsertReportsOlderVersionAsNotUpdated(t *testing.T) {
	conn := &upsertTestConn{hgetReply: []byte("existing")}
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	store := &redisDataStoreImpl{prefix: "test", pool: &upsertTestPool{conn: conn}, loggers: mockLog.Loggers}

	updated, err := store.Upsert(fixedVersionKind{version: 2}, "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        2,
		Deleted:        true,
		SerializedItem: []byte(`{"key":"flag-key","version":2,"deleted":true}`),
	})

	require.NoError(t, err)
	require.False(t, updated)
	mockLog.AssertMessageMatch(t, true, ldlog.Debug,
		`Attempted to delete key: flag-key .* with a version that is the same or older: 2`)
}

func TestUpsertReturnsScriptError(t *testing.T) {
	scriptErr := errors.New("script failed")
	conn := &upsertTestConn{scriptErr: scriptErr}
	store := &redisDataStoreImpl{
		prefix:  "test",
		pool:    &upsertTestPool{conn: conn},
		loggers: ldlog.NewDisabledLoggers(),
	}

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte(`{"key":"flag-key","version":1}`),
	})

	require.False(t, updated)
	require.ErrorIs(t, err, scriptErr)
	require.Equal(t, 1, conn.closeCount)
}

func TestUpsertReturnsReadError(t *testing.T) {
	readErr := errors.New("read failed")
	conn := &upsertTestConn{hgetErr: readErr}
	store := &redisDataStoreImpl{
		prefix:  "test",
		pool:    &upsertTestPool{conn: conn},
		loggers: ldlog.NewDisabledLoggers(),
	}

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte(`{"key":"flag-key","version":1}`),
	})

	require.False(t, updated)
	require.ErrorIs(t, err, readErr)
	require.Equal(t, 1, conn.closeCount)
}

func TestUpsertGivesUpAfterSameItemKeepsChanging(t *testing.T) {
	conn := &upsertTestConn{scriptReply: int64(0)}
	store := &redisDataStoreImpl{
		prefix:  "test",
		pool:    &upsertTestPool{conn: conn},
		loggers: ldlog.NewDisabledLoggers(),
	}

	updated, err := store.Upsert(ldstoreimpl.Features(), "flag-key", ldstoretypes.SerializedItemDescriptor{
		Version:        1,
		SerializedItem: []byte(`{"key":"flag-key","version":1}`),
	})

	require.False(t, updated)
	require.EqualError(t, err, `failed to update key "flag-key" in "features" after 10 attempts`)
	require.Equal(t, maxRetries, conn.closeCount)
}

type upsertTestPool struct{ conn *upsertTestConn }

func (p *upsertTestPool) Get() r.Conn  { return p.conn }
func (p *upsertTestPool) Close() error { return nil }

type upsertTestConn struct {
	hgetReply   interface{}
	hgetErr     error
	scriptReply interface{}
	scriptErr   error
	command     string
	args        []interface{}
	closeCount  int
}

func (c *upsertTestConn) Close() error {
	c.closeCount++
	return nil
}

func (c *upsertTestConn) Err() error { return nil }

func (c *upsertTestConn) Do(commandName string, args ...interface{}) (interface{}, error) {
	switch commandName {
	case "HGET":
		if c.hgetReply == nil && c.hgetErr == nil {
			return nil, r.ErrNil
		}
		return c.hgetReply, c.hgetErr
	case "EVALSHA":
		c.command = commandName
		c.args = args
		return c.scriptReply, c.scriptErr
	default:
		return nil, fmt.Errorf("unexpected Do command %q", commandName)
	}
}

func (c *upsertTestConn) Send(commandName string, _ ...interface{}) error {
	return fmt.Errorf("unexpected Send command %q", commandName)
}

func (c *upsertTestConn) Flush() error { return nil }

func (c *upsertTestConn) Receive() (interface{}, error) { return nil, nil }

type fixedVersionKind struct{ version int }

func (k fixedVersionKind) GetName() string { return "features" }

func (k fixedVersionKind) Serialize(ldstoretypes.ItemDescriptor) []byte { return nil }

func (k fixedVersionKind) Deserialize([]byte) (ldstoretypes.ItemDescriptor, error) {
	return ldstoretypes.ItemDescriptor{Version: k.version}, nil
}
