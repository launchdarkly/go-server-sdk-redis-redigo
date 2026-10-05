package ldredis

import (
	"bufio"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pingConn is a connection that counts the commands that it gets and returns pingErr for each one.
type pingConn struct {
	commands int
	pingErr  error
}

func (c *pingConn) Do(string, ...interface{}) (interface{}, error) {
	c.commands++
	if c.pingErr != nil {
		return nil, c.pingErr
	}
	return "PONG", nil
}

func (c *pingConn) Close() error                      { return nil }
func (c *pingConn) Err() error                        { return nil }
func (c *pingConn) Send(string, ...interface{}) error { return nil }
func (c *pingConn) Flush() error                      { return nil }
func (c *pingConn) Receive() (interface{}, error)     { return nil, nil }
func (c *pingConn) String() string                    { return "pingConn" }
func (c *pingConn) DoWithTimeout(time.Duration, string, ...interface{}) (interface{}, error) {
	return c.Do("")
}

func TestBorrowTesterAcceptsRecentlyReturnedConnectionWithoutPing(t *testing.T) {
	tester := &borrowTester{idleThreshold: time.Minute}
	conn := &pingConn{}

	assert.NoError(t, tester.test(conn, time.Now().Add(-time.Second)))
	assert.Equal(t, 0, conn.commands)
}

func TestBorrowTesterSendsPingToIdleConnection(t *testing.T) {
	tester := &borrowTester{idleThreshold: time.Minute}
	conn := &pingConn{}

	assert.NoError(t, tester.test(conn, time.Now().Add(-2*time.Minute)))
	assert.Equal(t, 1, conn.commands)
}

func TestBorrowTesterRejectsConnectionsIdleSinceFailedPing(t *testing.T) {
	tester := &borrowTester{idleThreshold: time.Minute}
	idleSince := time.Now().Add(-2 * time.Minute)
	pingErr := errors.New("i/o timeout")

	failing := &pingConn{pingErr: pingErr}
	assert.Equal(t, pingErr, tester.test(failing, idleSince))

	// This connection was idle when the PING failed, so it does not get a PING.
	alsoIdle := &pingConn{}
	assert.Equal(t, errIdleSinceFailedBorrowTest, tester.test(alsoIdle, idleSince))
	assert.Equal(t, 0, alsoIdle.commands)

	// This connection was returned after the PING failed, so it gets a PING as usual.
	returnedLater := &pingConn{}
	tester.idleThreshold = 0
	assert.NoError(t, tester.test(returnedLater, time.Now()))
	assert.Equal(t, 1, returnedLater.commands)
}

// stallingServer answers each command with +PONG until stall is set. After that it reads commands
// and never answers, like a Redis server that has stalled.
type stallingServer struct {
	listener net.Listener
	stall    atomic.Bool
}

func startStallingServer(t *testing.T) *stallingServer {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &stallingServer{listener: listener}
	var conns sync.WaitGroup
	t.Cleanup(func() {
		_ = listener.Close()
		conns.Wait()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go func() {
				defer conns.Done()
				s.serve(conn)
			}()
		}
	}()
	return s
}

func (s *stallingServer) url() string {
	return "redis://" + s.listener.Addr().String()
}

func (s *stallingServer) serve(conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	reader := bufio.NewReader(conn)
	for {
		if err := readCommand(reader); err != nil {
			return
		}
		if !s.stall.Load() {
			if _, err := conn.Write([]byte("+PONG\r\n")); err != nil {
				return
			}
		}
	}
}

// readCommand reads one command, which a client sends as an array of bulk strings.
func readCommand(reader *bufio.Reader) error {
	header, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "*")))
	if err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		lengthLine, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		length, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(lengthLine, "$")))
		if err != nil {
			return err
		}
		if _, err := reader.Discard(length + 2); err != nil {
			return err
		}
	}
	return nil
}

// Before the borrow tester, the pool sent a PING to each idle connection in turn, and a stalled server
// held the borrower for one read timeout per idle connection.
func TestPoolBorrowFromStalledServerTakesAtMostTwoReadTimeouts(t *testing.T) {
	const readTimeout = 250 * time.Millisecond
	const idleConnections = 8

	server := startStallingServer(t)
	pool := newPool(server.url(), []r.DialOption{r.DialReadTimeout(readTimeout)})
	// A threshold of zero makes the tester check every idle connection, as it does for connections that
	// were idle for longer than the default threshold.
	pool.TestOnBorrow = (&borrowTester{}).test
	t.Cleanup(func() { _ = pool.Close() })

	conns := make([]r.Conn, idleConnections)
	for i := range conns {
		conns[i] = pool.Get()
		_, err := conns[i].Do("PING")
		require.NoError(t, err)
	}
	for _, c := range conns {
		require.NoError(t, c.Close())
	}
	require.Equal(t, idleConnections, pool.IdleCount())

	server.stall.Store(true)
	start := time.Now()
	conn := pool.Get()
	_, err := conn.Do("PING")
	elapsed := time.Since(start)
	_ = conn.Close()

	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	assert.True(t, netErr.Timeout())
	// One PING to an idle connection, then one command on a new connection.
	assert.Less(t, elapsed, 3*readTimeout)
}
