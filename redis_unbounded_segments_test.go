package ldredis

import (
	"fmt"
	"testing"

	"gopkg.in/launchdarkly/go-server-sdk.v5/interfaces"
	"gopkg.in/launchdarkly/go-server-sdk.v5/testhelpers/storetest"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"
)

func TestUnboundedSegmentStore(t *testing.T) {
	client, err := r.DialURL(redisURL)
	require.NoError(t, err)
	defer client.Close()

	setTestMetadata := func(prefix string, metadata interfaces.UnboundedSegmentStoreMetadata) error {
		if prefix == "" {
			prefix = DefaultPrefix
		}
		_, err := client.Do("SET", unboundedSegmentsSyncTimeKey(prefix), fmt.Sprintf("%d", metadata.LastUpToDate))
		return err
	}

	setTestKeys := func(prefix string, userHashKey string, included []string, excluded []string) error {
		if prefix == "" {
			prefix = DefaultPrefix
		}
		for _, inc := range included {
			_, err := client.Do("SADD", unboundedSegmentsIncludeKey(prefix, userHashKey), inc)
			if err != nil {
				return err
			}
		}
		for _, exc := range excluded {
			_, err := client.Do("SADD", unboundedSegmentsExcludeKey(prefix, userHashKey), exc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	storetest.NewUnboundedSegmentStoreTestSuite(
		func(prefix string) interfaces.UnboundedSegmentStoreFactory {
			return DataStore().Prefix(prefix)
		},
		clearTestData,
		setTestMetadata,
		setTestKeys,
	).Run(t)
}
