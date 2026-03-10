# Change log

All notable changes to the LaunchDarkly Go SDK Redis integration will be documented in this file. This project adheres to [Semantic Versioning](http://semver.org).

## [3.0.0] - 2023-10-11
### Added:
- Added support for SDK v7 to support technology migrations.

## [2.0.1] - 2023-01-17
### Fixed:
- When using Big Segments, do not log a database error if the Big Segments status data has not yet been populated.

## [1.2.2] - 2023-01-17
### Fixed:
- When using Big Segments, do not log a database error if the Big Segments status data has not yet been populated.

## [2.0.0] - 2022-12-07
This release corresponds to the 6.0.0 release of the LaunchDarkly Go SDK. Any application code that is being updated to use the 6.0.0 SDK, and was using a 1.x version of `go-server-sdk-redis-redigo`, should now use a 2.x version instead.

There are no functional differences in the behavior of the Redis integration; the differences are only related to changes in the usage of interface types for configuration in the SDK.

### Added:
- `BigSegmentStore()`, which creates a configuration builder for use with Big Segments. Previously, the `DataStore()` builder was used for both regular data stores and Big Segment stores.

### Changed:
- The type `RedisDataStoreBuilder` has been removed, replaced by a generic type `RedisStoreBuilder`. Application code would not normally need to reference these types by name, but if necessary, use either `RedisStoreBuilder[PersistentDataStore]` or `RedisStoreBuilder[BigSegmentStore]` depending on whether you are configuring a regular data store or a Big Segment store.

## [1.2.1] - 2021-09-22
### Changed:
- When logging the Redis URL at startup, if the URL contains a password it is replaced by `xxxxx` (the same behavior as Go's `URL.Redacted()`).

## [1.2.0] - 2021-07-20
### Added:
- Added support for Big Segments. An Early Access Program for creating and syncing Big Segments from customer data platforms is available to enterprise customers.

## [1.1.0] - 2021-05-27
### Added:
- `DataStoreBuilder.PoolInterface()` is equivalent to `.Pool()`, but allows specifying the connection pool as an interface type rather than the concrete `*Pool` type from Redigo. (Thanks, [rafaeljusto](https://github.com/launchdarkly/go-server-sdk-redis-redigo/pull/5)!)

## [1.0.0] - 2020-09-18
Initial release of the stand-alone version of this package to be used with versions 5.0.0 and above of the LaunchDarkly Server-Side SDK for Go.
