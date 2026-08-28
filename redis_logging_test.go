package ldredis

import (
	"io"
	"strings"
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-sdk-common/v3/ldlogtest"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noHostMessage is the entire message logged for a Redis URL that logRedisURL cannot read a host
// from. No part of the URL appears in it.
const noHostMessage = "Could not read a host from the Redis URL. The URL is not logged because it may contain a password."

// loggingTest describes a Redis URL, the host that may reach the log, and the password that must
// never reach the log.
type loggingTest struct {
	name string
	url  string
	// wantHost is the host as the info message should report it. It is empty when logRedisURL
	// reads no host, because then nothing about the URL may be reported.
	wantHost string
	secret   string // empty when the URL carries no password
	// hostPiece is the piece of the password that url.Parse reads as the host. It is set only
	// for a URL that is ambiguous enough that a password and a host cannot be told apart, and
	// it is the one piece assertNoSecret allows in the log.
	hostPiece string
}

// urlsWithoutPassword covers URLs that url.Parse reads a host from and that carry no password.
func urlsWithoutPassword() []loggingTest {
	return []loggingTest{
		{
			name:     "host and port",
			url:      "redis://localhost:6379",
			wantHost: "localhost:6379",
		},
		{
			// The database number lives in the path, which is never logged.
			name:     "database number",
			url:      "redis://localhost:6379/1",
			wantHost: "localhost:6379",
		},
		{
			name:     "username with no password",
			url:      "redis://username@localhost:6379",
			wantHost: "localhost:6379",
		},
	}
}

// urlsWithPassword covers URLs that url.Parse reads a host from and that carry a password. The
// password lives outside the host in every one of them, so only the host is reported.
func urlsWithPassword() []loggingTest {
	return []loggingTest{
		{
			name:     "password",
			url:      "redis://username:very-s3cr3t@localhost:6379",
			wantHost: "localhost:6379",
			secret:   "very-s3cr3t",
		},
		{
			name:     "password with no username",
			url:      "redis://:very-s3cr3t@localhost:6379",
			wantHost: "localhost:6379",
			secret:   "very-s3cr3t",
		},
		{
			// url.Parse splits the authority on its last "@", so the whole password stays in
			// the userinfo and the host is still just the host.
			name:     "unescaped at sign in the password",
			url:      "redis://username:very@s3cr3t@localhost:6379",
			wantHost: "localhost:6379",
			secret:   "very@s3cr3t",
		},
		{
			// An unescaped "/" ends the authority, so url.Parse reads a host out of the middle
			// of the password and puts the real host in the path. Nothing can tell this URL
			// apart from one that means host "myhost.example", so the host it reports is wrong.
			// It still carries no password beyond the piece that reads as a host, which is the
			// property that matters here.
			name:      "unescaped at sign and slash in the password",
			url:       "redis://username:sup3r@myhost.example/v4lue@localhost:6379",
			wantHost:  "myhost.example",
			secret:    "sup3r@myhost.example/v4lue",
			hostPiece: "myhost.example",
		},
	}
}

// urlsWithNoHost covers URLs that url.Parse accepts and that have no authority component, so
// there is no host to log. The credentials land in the opaque part or in the path, where
// url.Redacted leaves them in the clear, which is why the host is the only thing ever logged.
func urlsWithNoHost() []loggingTest {
	return []loggingTest{
		{
			// An unescaped "/" right after the "@" ends the authority with an empty host.
			name:   "empty host",
			url:    "redis://username:very@/s3cr3t@localhost:6379",
			secret: "very@/s3cr3t",
		},
		{
			// A scheme with a single slash is hierarchical, so Opaque is empty, but there is
			// still no authority and the credentials land in the path.
			name:   "single slash after the scheme",
			url:    "redis:/username:very-s3cr3t@localhost:6379",
			secret: "very-s3cr3t",
		},
		{
			name:   "single slash and no scheme",
			url:    "/username:very-s3cr3t@localhost:6379",
			secret: "very-s3cr3t",
		},
		{
			// With no "//" the URL is opaque, so "username" reads as the scheme and the rest of
			// the URL, password included, is the opaque part.
			name:   "opaque URL with no scheme",
			url:    "username:very-s3cr3t@localhost:6379",
			secret: "very-s3cr3t",
		},
		{
			name:   "opaque URL with a scheme",
			url:    "redis:username:very-s3cr3t@localhost:6379",
			secret: "very-s3cr3t",
		},
		{
			name: "opaque URL with no credentials",
			url:  "localhost:6379",
		},
	}
}

// unparseableURLs covers URLs that url.Parse rejects, so there is nothing to read a host from.
func unparseableURLs() []loggingTest {
	return []loggingTest{
		{
			// A "%" that is not followed by two hex digits fails percent-decoding.
			name:   "bad percent encoding in the password",
			url:    "redis://username:very%ZZs3cr3t@localhost:6379",
			secret: "very%ZZs3cr3t",
		},
		{
			name:   "control character in the path",
			url:    "redis://username:very-s3cr3t@localhost:6379/\x7f",
			secret: "very-s3cr3t",
		},
		{
			name:   "malformed IPv6 host",
			url:    "redis://username:very-s3cr3t@[::1:6379",
			secret: "very-s3cr3t",
		},
	}
}

// storeBuilder builds one of the store types that log the Redis URL at startup.
type storeBuilder struct {
	name  string
	build func(redisURL string, context subsystems.ClientContext) (io.Closer, error)
}

// storeBuilders returns a builder for each store type that logs the Redis host. Both reach
// logRedisURL when the caller does not supply a custom pool.
func storeBuilders() []storeBuilder {
	return []storeBuilder{
		{
			name: "data store",
			build: func(redisURL string, context subsystems.ClientContext) (io.Closer, error) {
				return DataStore().URL(redisURL).Build(context)
			},
		},
		{
			name: "big segment store",
			build: func(redisURL string, context subsystems.ClientContext) (io.Closer, error) {
				return BigSegmentStore().URL(redisURL).Build(context)
			},
		},
	}
}

// runStartupLoggingTest builds every store type with the URL under test and checks the log output
// of each one. The whole output is compared, so anything reported at another level or in an extra
// message fails the test.
func runStartupLoggingTest(t *testing.T, test loggingTest) {
	t.Helper()

	want := ldlogtest.MockLogItem{Level: ldlog.Error, Message: noHostMessage}
	if test.wantHost != "" {
		want = ldlogtest.MockLogItem{Level: ldlog.Info, Message: "Using Redis host " + test.wantHost}
	}

	for _, builder := range storeBuilders() {
		t.Run(builder.name, func(t *testing.T) {
			mockLog := ldlogtest.NewMockLog()
			defer mockLog.DumpIfTestFailed(t)
			var context subsystems.BasicClientContext
			context.Logging.Loggers = mockLog.Loggers

			store, err := builder.build(test.url, context)
			require.NoError(t, err)
			_ = store.Close()

			assert.Equal(t, []ldlogtest.MockLogItem{want}, mockLog.GetAllOutput())
			assertNoSecret(t, mockLog, test)
		})
	}
}

// assertNoSecret checks that no part of the password reached the log at any level. A password can
// hold a delimiter, so every piece between delimiters is checked and not just the whole password.
// The one piece allowed through is the one url.Parse reads as the host.
func assertNoSecret(t *testing.T, mockLog *ldlogtest.MockLog, test loggingTest) {
	t.Helper()

	if test.secret == "" {
		return
	}
	pieces := append([]string{test.secret}, strings.FieldsFunc(test.secret, func(c rune) bool {
		return strings.ContainsRune(":/?#@%", c)
	})...)
	for _, item := range mockLog.GetAllOutput() {
		for _, piece := range pieces {
			if piece == test.hostPiece {
				continue
			}
			assert.NotContains(t, item.Message, piece, "the password reached the log")
		}
	}
}

func TestRedisHostAppearsInLogAtStartup(t *testing.T) {
	for _, test := range urlsWithoutPassword() {
		t.Run(test.name, func(t *testing.T) {
			runStartupLoggingTest(t, test)
		})
	}
}

// TestOnlyTheRedisHostAppearsInLogAtStartup covers the branch that used to report the whole URL
// through url.Redacted, which left the password in the clear for several URL shapes.
func TestOnlyTheRedisHostAppearsInLogAtStartup(t *testing.T) {
	for _, test := range urlsWithPassword() {
		t.Run(test.name, func(t *testing.T) {
			require.NotEmpty(t, test.secret, "this group covers URLs that carry a password")
			runStartupLoggingTest(t, test)
		})
	}
}

// TestURLIsNotLoggedWhenItHasNoHost covers URLs that parse without an authority component. Every
// one of them is a URL that url.Redacted reports in full, password included.
func TestURLIsNotLoggedWhenItHasNoHost(t *testing.T) {
	for _, test := range urlsWithNoHost() {
		t.Run(test.name, func(t *testing.T) {
			require.Empty(t, test.wantHost, "a URL with no host may not be reported")
			runStartupLoggingTest(t, test)
		})
	}
}

// TestURLIsNotLoggedWhenItCannotBeParsed covers the branch that used to write the whole URL,
// password included, to the log.
func TestURLIsNotLoggedWhenItCannotBeParsed(t *testing.T) {
	for _, test := range unparseableURLs() {
		t.Run(test.name, func(t *testing.T) {
			require.Empty(t, test.wantHost, "an unparseable URL may not be reported")
			runStartupLoggingTest(t, test)
		})
	}
}
