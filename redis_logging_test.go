package ldredis

import (
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-sdk-common/v3/ldlogtest"
	"github.com/launchdarkly/go-server-sdk/v6/ldcomponents"
	"github.com/launchdarkly/go-server-sdk/v6/testhelpers"

	"github.com/stretchr/testify/require"
)

func doStartupLoggingTest(t *testing.T, url string, expectedLogURL string) {
	mockLog1 := ldlogtest.NewMockLog()
	mockLog2 := ldlogtest.NewMockLog()
	defer mockLog1.DumpIfTestFailed(t)
	defer mockLog2.DumpIfTestFailed(t)
	context1 := testhelpers.NewSimpleClientContext("sdk-key").
		WithLogging(ldcomponents.Logging().Loggers(mockLog1.Loggers))
	context2 := testhelpers.NewSimpleClientContext("sdk-key").
		WithLogging(ldcomponents.Logging().Loggers(mockLog2.Loggers))

	store1, err := DataStore().URL(url).CreatePersistentDataStore(context1)
	require.NoError(t, err)
	_ = store1.Close()
	mockLog1.AssertMessageMatch(t, true, ldlog.Info, "Using URL: "+expectedLogURL)

	store2, err := DataStore().URL(url).CreateBigSegmentStore(context2)
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
