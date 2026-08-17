package ldredis

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
)

// UpsertModeAtomicScript retries only when the same item changes during an attempt, which makes the
// retry budget hard to exhaust. These two tests bound that claim from both sides: the first builds
// the shape that does exhaust it, and the second hammers one item through the public API and shows
// that it does not. The budget for UpsertModeWatch is covered by the unit tests, which can drive it
// without a real server.

// TestUpsertRetryBudgetIsReachable exhausts the budget with the only shape that can: ten successive
// changes to the same item during one update, each landing a version still older than the one being
// written, so the version check never refuses and the value never matches.
//
// Broadcast stream delivery does not produce that pattern, but it is reachable, so the error the
// budget produces is real rather than decorative.
func TestUpsertRetryBudgetIsReachable(t *testing.T) {
	const prefix = "test-retry-budget-reachable"
	const key = "flag-key"
	require.NoError(t, clearTestData(prefix))
	t.Cleanup(func() { require.NoError(t, clearTestData(prefix)) })

	kind := ldstoreimpl.Features()
	store := newTestStore(t, prefix, UpsertModeAtomicScript)
	other := newTestStore(t, prefix, UpsertModeAtomicScript)

	require.NoError(t, store.Init(initOneItem(kind, key, 1)))

	// Every attempt sees a fresh but still older version, so it always reaches the script, and the
	// script always finds a value other than the one the attempt expected.
	version := 1
	attempts := 0
	store.testTxHook = func() {
		attempts++
		version++
		updated, err := other.Upsert(kind, key, serializedFlag(key, version, 64))
		require.NoError(t, err)
		require.True(t, updated)
	}

	updated, err := store.Upsert(kind, key, serializedFlag(key, 1000, 64))

	require.False(t, updated)
	require.EqualError(t, err,
		fmt.Sprintf("failed to update key %q in %q after %d attempts", key, "features", maxRetries))
	require.Equal(t, maxRetries, attempts, "every attempt must have reached the script")

	// The store holds the last interfering write, not something partial.
	got, err := store.Get(kind, key)
	require.NoError(t, err)
	parsed, err := kind.Deserialize(got.SerializedItem)
	require.NoError(t, err)
	require.Equal(t, version, parsed.Version)
}

// TestUpsertRetryBudgetSurvivesRealContention hammers one item through the public API, with no hook
// and no imposed ordering, and requires that no update ever runs out of attempts. Whichever writer
// loses a race reads the winner's newer version on its next attempt and stops at the version check
// instead of retrying, which is why the budget holds.
func TestUpsertRetryBudgetSurvivesRealContention(t *testing.T) {
	const prefix = "test-retry-budget-contention"
	const key = "flag-key"
	const writers = 24
	const perWriter = 150
	require.NoError(t, clearTestData(prefix))
	t.Cleanup(func() { require.NoError(t, clearTestData(prefix)) })

	kind := ldstoreimpl.Features()
	stores := make([]*redisDataStoreImpl, writers)
	for i := range stores {
		stores[i] = newTestStore(t, prefix, UpsertModeAtomicScript)
	}
	require.NoError(t, stores[0].Init(initOneItem(kind, key, 1)))

	var mu sync.Mutex
	var failures []error
	var wrote, superseded int

	start := make(chan struct{})
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			<-start
			for j := range perWriter {
				// Interleaving the versions across writers keeps the item genuinely contended.
				version := 2 + j*writers + w
				updated, err := stores[w].Upsert(kind, key, serializedFlag(key, version, 2<<10))
				mu.Lock()
				switch {
				case err != nil:
					failures = append(failures, err)
				case updated:
					wrote++
				default:
					superseded++
				}
				mu.Unlock()
			}
		}(w)
	}
	close(start)
	wg.Wait()

	total := writers * perWriter
	require.Empty(t, failures,
		"contention on the same item must not exhaust the retry budget through the public API")
	require.Equal(t, total, wrote+superseded)
	require.Positive(t, wrote, "at least some writes must have landed")
	require.Positive(t, superseded, "the contention must have been real")
}

func initOneItem(kind ldstoretypes.DataKind, key string, version int) []ldstoretypes.SerializedCollection {
	return []ldstoretypes.SerializedCollection{{
		Kind: kind,
		Items: []ldstoretypes.KeyedSerializedItemDescriptor{
			{Key: key, Item: serializedFlag(key, version, 64)},
		},
	}}
}
