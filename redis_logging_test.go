package ldredis

import (
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-sdk-common/v3/ldlogtest"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems"

	"github.com/stretchr/testify/require"
)

func doStartupLoggingTest(t *testing.T, url string, expectedLogURL string) {
	mockLog1 := ldlogtest.NewMockLog()
	mockLog2 := ldlogtest.NewMockLog()
	defer mockLog1.DumpIfTestFailed(t)
	defer mockLog2.DumpIfTestFailed(t)
	var context1, context2 subsystems.BasicClientContext
	context1.Logging.Loggers = mockLog1.Loggers
	context2.Logging.Loggers = mockLog2.Loggers

	store1, err := DataStore().URL(url).Build(context1)
	require.NoError(t, err)
	_ = store1.Close()
	mockLog1.AssertMessageMatch(t, true, ldlog.Info, "Using URL: "+expectedLogURL)

	store2, err := DataStore().URL(url).Build(context2)
	require.NoError(t, err)
	_ = store2.Close()
	mockLog2.AssertMessageMatch(t, true, ldlog.Info, "Using URL: "+expectedLogURL)
}

func TestURLAppearsInLogAtStartup(t *testing.T) {
	doStartupLoggingTest(t, "redis://localhost:6379", "redis://localhost:6379")
	doStartupLoggingTest(t, "redis://localhost:6379/1", "redis://localhost:6379/1")
}

func TestURLPasswordIsObfuscatedInLog(t *testing.T) {
	doStartupLoggingTest(t, "redis://username@localhost:6379", "redis://username@localhost:6379")
	doStartupLoggingTest(t, "redis://username:very-secret-password@localhost:6379", "redis://username:xxxxx@localhost:6379")
}
