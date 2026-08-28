package ldredis

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
)

// TestUpsertConcurrentDistinctItems is the workload the atomic script exists for: many writers
// updating different items of the same kind at the same time. Every write has to succeed, because no
// two writers ever touch the same item. UpsertModeWatch watches the whole kind rather than the item,
// so this workload makes it retry and, often enough to matter, run out of attempts; the contention
// benchmarks measure how often. That is why this test runs only in UpsertModeAtomicScript.
func TestUpsertConcurrentDistinctItems(t *testing.T) {
	for _, writers := range []int{8, 64} {
		t.Run(fmt.Sprintf("%d writers", writers), func(t *testing.T) {
			const writeCount = 100
			prefix := fmt.Sprintf("test-concurrent-distinct-%d", writers)
			require.NoError(t, clearTestData(prefix))
			t.Cleanup(func() { require.NoError(t, clearTestData(prefix)) })

			store := newTestStore(t, prefix, UpsertModeAtomicScript)

			start := make(chan struct{})
			errs := make(chan error, writers)
			var wait sync.WaitGroup
			for writer := range writers {
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					key := fmt.Sprintf("flag-%d", writer)
					for version := 1; version <= writeCount; version++ {
						updated, err := store.Upsert(ldstoreimpl.Features(), key,
							serializedFlag(key, version, 256))
						if err != nil {
							errs <- err
							return
						}
						if !updated {
							errs <- fmt.Errorf("writer %d version %d was not updated", writer, version)
							return
						}
					}
				}()
			}
			close(start)
			wait.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
		})
	}
}
