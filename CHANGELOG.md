# Change log

All notable changes to the LaunchDarkly Go SDK Redis integration will be documented in this file. This project adheres to [Semantic Versioning](http://semver.org).

## [4.0.0](https://github.com/launchdarkly/go-server-sdk-redis-redigo/compare/v3.0.5...v4.0.0) (2026-08-28)


### ⚠ BREAKING CHANGES

* Compare and set one item atomically by default ([#56](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/56))
* Split StoreBuilder into DataStoreBuilder and BigSegmentStoreBuilder ([#55](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/55))
* Move module path to v4 ([#54](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/54))

### Features

* Compare and set one item atomically by default ([#56](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/56)) ([8f8b901](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/8f8b901f3b7d2d6c67f4e4f83d2d8cb7676f72e4))
* Move module path to v4 ([#54](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/54)) ([ee7948d](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/ee7948d56aa037a7217253c1d6dc77b08b31a4a7))
* Split StoreBuilder into DataStoreBuilder and BigSegmentStoreBuilder ([#55](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/55)) ([20a9fb1](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/20a9fb14e2d06f349b48d430ab5680adf5ba0cb1))


### Bug Fixes

* Probe the script when checking store availability ([#57](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/57)) ([b6d9a58](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/b6d9a58853c308b13ce881eff646398af956afe0))
* Stop logging a Redis URL that cannot be parsed ([#61](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/61)) ([6decd27](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/6decd27fedb4699a41ba2c30d4ca4f150dc5bfc5))

## [3.0.5](https://github.com/launchdarkly/go-server-sdk-redis-redigo/compare/v3.0.4...v3.0.5) (2026-08-06)


### Bug Fixes

* Bound the number of Upsert retry attempts ([#52](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/52)) ([2fcd731](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/2fcd73118c4d6b0187d2dd808b6bcbbcc10b0b8f))
* Propagate top-level Redis EXEC errors from Upsert ([#50](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/50)) ([57588dd](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/57588dda7607ed25c5ffcb4befe400a07f305215))
* Release Redis connections between Upsert retries ([#49](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/49)) ([07a8725](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/07a87252e6b359de0939863ca20a64af36d2c26c))

## [3.0.4](https://github.com/launchdarkly/go-server-sdk-redis-redigo/compare/v3.0.3...v3.0.4) (2026-06-11)


### Bug Fixes

* **deps:** revert v4 core libraries to v3 ([#46](https://github.com/launchdarkly/go-server-sdk-redis-redigo/issues/46)) ([2a32192](https://github.com/launchdarkly/go-server-sdk-redis-redigo/commit/2a3219254627c6af435c8b80fd25f0b012c9ba4e))

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
