package ldredis

import (
	"fmt"
	"testing"

	"github.com/launchdarkly/go-server-sdk/v7/subsystems"
	"github.com/launchdarkly/go-server-sdk/v7/testhelpers/storetest"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"
)

func TestBigSegmentStore(t *testing.T) {
	client, err := r.DialURL(redisURL)
	require.NoError(t, err)
	defer client.Close()

	setTestMetadata := func(prefix string, metadata subsystems.BigSegmentStoreMetadata) error {
		if prefix == "" {
			prefix = DefaultPrefix
		}
		_, err := client.Do("SET", bigSegmentsSyncTimeKey(prefix), fmt.Sprintf("%d", metadata.LastUpToDate))
		return err
	}

	setTestSegments := func(prefix string, contextHashKey string, included []string, excluded []string) error {
		if prefix == "" {
			prefix = DefaultPrefix
		}
		for _, inc := range included {
			_, err := client.Do("SADD", bigSegmentsIncludeKey(prefix, contextHashKey), inc)
			if err != nil {
				return err
			}
		}
		for _, exc := range excluded {
			_, err := client.Do("SADD", bigSegmentsExcludeKey(prefix, contextHashKey), exc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	storetest.NewBigSegmentStoreTestSuite(
		func(prefix string) subsystems.ComponentConfigurer[subsystems.BigSegmentStore] {
			return BigSegmentStore().Prefix(prefix)
		},
		clearTestData,
		setTestMetadata,
		setTestSegments,
	).Run(t)
}
