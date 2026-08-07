package ldredis

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoreimpl"
	"github.com/launchdarkly/go-server-sdk/v7/subsystems/ldstoretypes"
)

func TestUpsertConcurrentDistinctFields(t *testing.T) {
	for _, writerCount := range []int{8, 64} {
		t.Run(fmt.Sprintf("%d writers", writerCount), func(t *testing.T) {
			testUpsertConcurrentDistinctFields(t, writerCount)
		})
	}
}

func testUpsertConcurrentDistinctFields(t *testing.T, writerCount int) {
	const writeCount = 100
	prefix := fmt.Sprintf("atomic-upsert-concurrency-%d", writerCount)
	store := newBenchmarkStore(t, prefix)
	defer store.Close() // nolint:errcheck
	require.NoError(t, clearTestData(prefix))

	start := make(chan struct{})
	errs := make(chan error, writerCount)
	var wait sync.WaitGroup
	for writer := range writerCount {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			key := fmt.Sprintf("flag-%d", writer)
			for version := 1; version <= writeCount; version++ {
				updated, err := store.Upsert(ldstoreimpl.Features(), key, serializedFlag(key, version, 256))
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
}

func BenchmarkUpsertConcurrent(b *testing.B) {
	for _, size := range []struct {
		name      string
		flagCount int
		valueSize int
	}{
		{name: "medium-1k-flags-2KiB", flagCount: 1_000, valueSize: 2 << 10},
		{name: "large-10k-flags-16KiB", flagCount: 10_000, valueSize: 16 << 10},
	} {
		b.Run(size.name, func(b *testing.B) {
			const writerCount = 8
			prefix := "atomic-upsert-benchmark-" + size.name
			store := newBenchmarkStore(b, prefix)
			defer store.Close() // nolint:errcheck
			require.NoError(b, clearTestData(prefix))
			seedFlags(b, store, size.flagCount, size.valueSize)

			var next atomic.Int64
			var wait sync.WaitGroup
			start := make(chan struct{})
			errs := make(chan error, writerCount)
			b.ResetTimer()
			for writer := range writerCount {
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					key := fmt.Sprintf("flag-%d", writer)
					for {
						op := int(next.Add(1))
						if op > b.N {
							return
						}
						version := op + 1
						updated, err := store.Upsert(ldstoreimpl.Features(), key,
							serializedFlag(key, version, size.valueSize))
						if err != nil {
							errs <- err
							return
						}
						if !updated {
							errs <- fmt.Errorf("version %d was not updated", version)
							return
						}
					}
				}()
			}
			close(start)
			wait.Wait()
			b.StopTimer()
			close(errs)
			for err := range errs {
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkUpsertConcurrentSameFlag(b *testing.B) {
	for _, size := range []struct {
		name      string
		flagCount int
		valueSize int
	}{
		{name: "medium-1k-flags-2KiB", flagCount: 1_000, valueSize: 2 << 10},
		{name: "large-10k-flags-16KiB", flagCount: 10_000, valueSize: 16 << 10},
	} {
		b.Run(size.name, func(b *testing.B) {
			const writerCount = 8
			prefix := "atomic-upsert-same-flag-benchmark-" + size.name
			store := newBenchmarkStore(b, prefix)
			defer store.Close() // nolint:errcheck
			require.NoError(b, clearTestData(prefix))
			seedFlags(b, store, size.flagCount, size.valueSize)

			var next atomic.Int64
			var errorsCount atomic.Int64
			var superseded atomic.Int64
			var wait sync.WaitGroup
			start := make(chan struct{})
			b.ResetTimer()
			for range writerCount {
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					for {
						op := int(next.Add(1))
						if op > b.N {
							return
						}
						updated, err := store.Upsert(ldstoreimpl.Features(), "flag-0",
							serializedFlag("flag-0", op+1, size.valueSize))
						if err != nil {
							errorsCount.Add(1)
						} else if !updated {
							superseded.Add(1)
						}
					}
				}()
			}
			close(start)
			wait.Wait()
			b.StopTimer()
			b.ReportMetric(float64(errorsCount.Load())/float64(b.N)*100, "errors_%")
			b.ReportMetric(float64(superseded.Load())/float64(b.N)*100, "superseded_%")
		})
	}
}

type testingTB interface {
	Helper()
	Fatalf(string, ...interface{})
}

func newBenchmarkStore(t testingTB, prefix string) *redisDataStoreImpl {
	t.Helper()
	return &redisDataStoreImpl{
		prefix:  prefix,
		pool:    newPool(redisURL, nil),
		loggers: ldlog.NewDisabledLoggers(),
	}
}

func seedFlags(t testingTB, store *redisDataStoreImpl, count int, valueSize int) {
	t.Helper()
	c := store.getConn()
	defer c.Close() // nolint:errcheck
	for i := range count {
		key := fmt.Sprintf("flag-%d", i)
		if err := c.Send("HSET", store.featuresKey(ldstoreimpl.Features()), key,
			serializedFlag(key, 1, valueSize).SerializedItem); err != nil {
			t.Fatalf("failed to queue seed flag: %v", err)
		}
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("failed to seed flags: %v", err)
	}
	for range count {
		if _, err := c.Receive(); err != nil {
			t.Fatalf("failed to receive seed response: %v", err)
		}
	}
}

func serializedFlag(key string, version int, valueSize int) ldstoretypes.SerializedItemDescriptor {
	paddingSize := max(0, valueSize-len(key)-64)
	serialized := fmt.Appendf(nil, `{"key":%q,"version":%d,"padding":"%0*s"}`,
		key, version, paddingSize, "")
	return ldstoretypes.SerializedItemDescriptor{Version: version, SerializedItem: serialized}
}
