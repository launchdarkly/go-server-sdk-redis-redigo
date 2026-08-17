package ldredis

import (
	"fmt"
	"testing"

	r "github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
)

// TestUpsertDoesNotClobberItemCreatedDuringAttempt covers what happens when Upsert reads an item as
// absent and another client creates it before the update commits. The version it read is no version
// at all, so nothing about the versions can stop the write; only noticing that the item now exists
// can. The shared upstream suite cannot reach this case, because every one of its concurrent
// modification cases starts from an Init that creates the item.
func TestUpsertDoesNotClobberItemCreatedDuringAttempt(t *testing.T) {
	for _, m := range upsertModes {
		t.Run(m.name, func(t *testing.T) {
			const key = "flag-key"
			prefix := fmt.Sprintf("test-absent-race-%d", m.mode)
			require.NoError(t, clearTestData(prefix))
			t.Cleanup(func() { require.NoError(t, clearTestData(prefix)) })

			store := newTestStore(t, prefix, m.mode)
			other := newTestStore(t, prefix, m.mode)

			// store reads the item as absent; another client creates version 5 before it can write.
			fired := false
			store.testTxHook = func() {
				if fired {
					return
				}
				fired = true
				updated, err := other.Upsert(ldstoreimpl.Features(), key, serializedFlag(key, 5, 64))
				require.NoError(t, err)
				require.True(t, updated)
			}

			updated, err := store.Upsert(ldstoreimpl.Features(), key, serializedFlag(key, 2, 64))
			require.NoError(t, err)
			require.True(t, fired, "the hook must have run, otherwise the race was not set up")
			require.False(t, updated, "version 2 must not replace the version 5 written during the attempt")

			final, err := store.Get(ldstoreimpl.Features(), key)
			require.NoError(t, err)
			parsed, err := ldstoreimpl.Features().Deserialize(final.SerializedItem)
			require.NoError(t, err)
			require.Equal(t, 5, parsed.Version, "the newer item must survive")
		})
	}
}

// TestInitRacingUpsert forces an Init to commit while an Upsert of the same kind is in flight. Init
// replaces the whole hash for a kind, so under UpsertModeWatch it invalidates every Upsert of that
// kind, while UpsertModeAtomicScript only cares about the one item. The two modes therefore differ in
// how many attempts each case costs, but both have to reach the same state, which is what these cases
// pin down.
func TestInitRacingUpsert(t *testing.T) {
	kind := ldstoreimpl.Features()
	const key = "flag-key"

	initWith := func(items ...ldstoretypes.KeyedSerializedItemDescriptor) []ldstoretypes.SerializedCollection {
		return []ldstoretypes.SerializedCollection{{Kind: kind, Items: items}}
	}
	item := func(k string, v int) ldstoretypes.KeyedSerializedItemDescriptor {
		return ldstoretypes.KeyedSerializedItemDescriptor{Key: k, Item: serializedFlag(k, v, 64)}
	}
	versionOf := func(t *testing.T, store *redisDataStoreImpl, k string) int {
		t.Helper()
		got, err := store.Get(kind, k)
		require.NoError(t, err)
		if got.SerializedItem == nil {
			return -1
		}
		parsed, err := kind.Deserialize(got.SerializedItem)
		require.NoError(t, err)
		return parsed.Version
	}

	cases := []struct {
		name        string
		seed        int // version the store holds before the Upsert starts
		initItems   []ldstoretypes.KeyedSerializedItemDescriptor
		upsertTo    int
		wantUpdated bool
		wantVersion int
		note        string
	}{
		{
			name:        "Init installs a newer version of the same item",
			seed:        5,
			initItems:   []ldstoretypes.KeyedSerializedItemDescriptor{item(key, 20)},
			upsertTo:    6,
			wantUpdated: false,
			wantVersion: 20,
			note:        "the update must be refused, because 6 is older than the 20 that Init installed",
		},
		{
			name:        "Init installs an older version of the same item",
			seed:        5,
			initItems:   []ldstoretypes.KeyedSerializedItemDescriptor{item(key, 3)},
			upsertTo:    6,
			wantUpdated: true,
			wantVersion: 6,
			note:        "Init reset the store to 3, so advancing to 6 is correct",
		},
		{
			name:        "Init installs a byte-identical value for the same item",
			seed:        5,
			initItems:   []ldstoretypes.KeyedSerializedItemDescriptor{item(key, 5)},
			upsertTo:    6,
			wantUpdated: true,
			wantVersion: 6,
			note:        "the script writes without reading again, so it never observes this Init",
		},
		{
			name:        "Init drops the item entirely",
			seed:        5,
			initItems:   []ldstoretypes.KeyedSerializedItemDescriptor{item("other", 1)},
			upsertTo:    6,
			wantUpdated: true,
			wantVersion: 6,
			note:        "the item is resurrected into the freshly initialized store",
		},
		{
			name:        "Init only touches a different item",
			seed:        5,
			initItems:   []ldstoretypes.KeyedSerializedItemDescriptor{item(key, 5), item("other", 9)},
			upsertTo:    6,
			wantUpdated: true,
			wantVersion: 6,
			note:        "an unrelated item must not affect this update",
		},
	}

	for _, m := range upsertModes {
		t.Run(m.name, func(t *testing.T) {
			for i, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					prefix := fmt.Sprintf("test-init-race-%d-%d", m.mode, i)
					require.NoError(t, clearTestData(prefix))
					t.Cleanup(func() { require.NoError(t, clearTestData(prefix)) })

					store := newTestStore(t, prefix, m.mode)
					initer := newTestStore(t, prefix, m.mode)

					if tc.seed >= 0 {
						require.NoError(t, store.Init(initWith(item(key, tc.seed))))
					} else {
						require.NoError(t, store.Init(initWith()))
					}

					fired := false
					store.testTxHook = func() {
						if fired {
							return
						}
						fired = true
						require.NoError(t, initer.Init(initWith(tc.initItems...)))
					}

					updated, err := store.Upsert(kind, key, serializedFlag(key, tc.upsertTo, 64))
					require.NoError(t, err, "an Init racing an update must not surface an error")
					require.True(t, fired, "the Init must have landed inside the window of the update")

					require.Equal(t, tc.wantUpdated, updated, tc.note)
					require.Equal(t, tc.wantVersion, versionOf(t, store, key), tc.note)

					// Init is atomic, so whatever it installed for other items is still there.
					for _, it := range tc.initItems {
						if it.Key == key {
							continue
						}
						got, err := store.Get(kind, it.Key)
						require.NoError(t, err)
						require.NotNil(t, got.SerializedItem, "expected %q to exist", it.Key)
						parsed, err := kind.Deserialize(got.SerializedItem)
						require.NoError(t, err)
						expected, err := kind.Deserialize(it.Item.SerializedItem)
						require.NoError(t, err)
						require.Equal(t, expected.Version, parsed.Version,
							"the other items Init wrote must survive the concurrent update")
					}

					c := store.getConn()
					defer c.Close() //nolint:errcheck // test cleanup
					inited, err := r.Bool(c.Do("EXISTS", store.initedKey()))
					require.NoError(t, err)
					require.True(t, inited, "the initialized marker must survive")
				})
			}
		})
	}
}
