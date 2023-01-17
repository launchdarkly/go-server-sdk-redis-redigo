# Change log

All notable changes to the LaunchDarkly Go SDK Redis integration will be documented in this file. This project adheres to [Semantic Versioning](http://semver.org).

## [1.2.2] - 2023-01-17
### Fixed:
- When using Big Segments, do not log a database error if the Big Segments status data has not yet been populated.

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
