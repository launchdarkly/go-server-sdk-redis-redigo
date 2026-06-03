# Change log

All notable changes to the LaunchDarkly Go SDK Redis integration will be documented in this file. This project adheres to [Semantic Versioning](http://semver.org).

## [3.0.3](https://github.com/launchdarkly/go-server-sdk-redis-redigo/compare/v3.0.2...v3.0.3) (2026-06-03)


### Bug Fixes

* bump SDK deps for EasyJSON removal (v4 cascade) ([#42](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/42)) ([8f543f2](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/8f543f228f2767327b1e1dd0f5dde72f1c7464f5))

## [3.0.2](https://github.com/launchdarkly/go-server-sdk-redis-redigo/compare/v3.0.1...v3.0.2) (2026-05-04)


### Bug Fixes

* Reuse connection in Upsert to prevent deadlock ([#39](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/39)) ([a1bd118](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/a1bd11827e2d36d3dcb9559ed8e398667d556ba8))

## [3.0.1](https://github.com/launchdarkly/go-server-sdk-redis-redigo/compare/v3.0.0...v3.0.1) (2026-03-10)


### Bug Fixes

* Bump gopkg.in/yaml.v3 from 3.0.0 to 3.0.1 ([#30](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/30)) ([f4e4033](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/f4e40336e9e679c61756970072e144b70a4b18f6))

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
