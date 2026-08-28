# LaunchDarkly Server-side SDK for Go - Redis integration with Redigo client

[![Actions Status](https://github.com/launchdarkly/go-server-sdk-redis-redigo/actions/workflows/ci.yml/badge.svg?branch=v4)](https://github.com/launchdarkly/go-server-sdk-redis-redigo/actions/workflows/ci.yml)
[![Documentation](https://img.shields.io/static/v1?label=go.dev&message=reference&color=00add8)](https://pkg.go.dev/github.com/launchdarkly/go-server-sdk-redis-redigo/v4)

This library provides a [Redis](https://redis.io/)-backed persistence mechanism (data store) for the [LaunchDarkly Go SDK](https://github.com/launchdarkly/go-server-sdk), replacing the default in-memory data store.

The Redis API implementation it uses is [Redigo](https://github.com/gomodule/redigo). There are other Redis client implementations for Go; if LaunchDarkly SDK Redis integrations using other Redis clients are released, they will be in separate repositories.

For more information, see also: [Using a persistent feature store](https://docs.launchdarkly.com/v2.0/docs/using-a-persistent-feature-store).

## Requirements

* Version 6.0.0 or higher of the LaunchDarkly Go SDK. For versions of this library to use with earlier SDK versions, see the changelog.
* Go 1.24 or higher.
* Permission to run Lua scripts on the Redis server: the `EVAL` and `EVALSHA` commands, or the `+@scripting` ACL category. The data store needs this to update one flag without losing a concurrent update of another flag. Default Redis and Valkey allow it, as do the major managed offerings.

  If your Redis server cannot run scripts, because a command allowlist omits them or a Redis-protocol proxy does not implement them, select `ldredis.UpsertModeWatch` as described in [Concurrent updates](#concurrent-updates). Nothing falls back to that mode on its own: without the permission, every update fails and the SDK reports the store as unavailable.
* Access to the Redis keys under the configured prefix. Along with the keys that hold data, the default mode reads `PREFIX:$availability` to check that it can still run scripts. It never writes that key, and the key never comes into existence.

## Quick setup

This assumes that you have already installed the LaunchDarkly Go SDK.

1. Import the LaunchDarkly SDK packages and the package for this library:

```go
import (
    ld "github.com/launchdarkly/go-server-sdk/v7"
    "github.com/launchdarkly/go-server-sdk/v7/ldcomponents"
    ldredis "github.com/launchdarkly/go-server-sdk-redis-redigo/v4"
)
```

2. When configuring your SDK client, add the Redis data store as a `PersistentDataStore`. You may specify any custom Redis options using the methods of `DataStoreBuilder`. For instance, to customize the Redis URL:

```go
    var config ld.Config
    config.DataStore = ldcomponents.PersistentDataStore(
        ldredis.DataStore().URL("redis://my-redis-host"),
    )
```

By default, the store will try to connect to a local Redis instance on port 6379.

## Caching behavior

The LaunchDarkly SDK has a standard caching mechanism for any persistent data store, to reduce database traffic. This is configured through the SDK's `PersistentDataStoreBuilder` as described in the SDK documentation. For instance, to specify a cache TTL of 5 minutes:

```go
    var config ld.Config
    config.DataStore = ldcomponents.PersistentDataStore(
        ldredis.DataStore(),
    ).CacheSeconds(300)
```

## Concurrent updates

Every SDK instance that shares a Redis database writes flag data to it, so the store has to update one flag without losing an update of another flag that is happening at the same time. There are two ways to do that, and `UpsertMode` selects between them.

`ldredis.UpsertModeAtomicScript` is the default. It compares and sets a single flag with a Lua script, so updates of different flags never contend with each other. It needs permission to run scripts, as described in [Requirements](#requirements).

`ldredis.UpsertModeWatch` is what version 3 of this library did, and it is the escape hatch for a server that cannot run scripts:

```go
    var config ld.Config
    config.DataStore = ldcomponents.PersistentDataStore(
        ldredis.DataStore().UpsertMode(ldredis.UpsertModeWatch),
    )
```

`WATCH` covers the whole Redis hash that holds one kind of data rather than the individual flag, so updating any flag makes every in-flight update of that kind start over. A burst of flag changes, or several SDK instances reconnecting at once, can use up the attempts an update is allowed. The update then fails, the SDK reports the store as unavailable, and the data source restarts the stream and writes the whole environment again, logging `Restarting stream to refresh data after data store outage`. Reads keep working throughout, so evaluation looks healthy and only the logs show the problem. Those failures are what the default mode removes, so choose this mode only when scripting is unavailable.

Both modes end in the same state. They differ in one observable way: under `UpsertModeWatch`, a full refresh of the data that rewrites a flag with byte-identical content makes an in-flight update of that flag read it again, and under `UpsertModeAtomicScript` it does not.

## Migrating from version 3

Version 4 has three breaking changes.

**1. The import path.** Update it wherever this library is imported, and in `go.mod`:

```go
// before
import ldredis "github.com/launchdarkly/go-server-sdk-redis-redigo/v3"

// after
import ldredis "github.com/launchdarkly/go-server-sdk-redis-redigo/v4"
```

**2. `StoreBuilder[T]` is gone.** `DataStore()` now returns `*DataStoreBuilder` and `BigSegmentStore()` now returns `*BigSegmentStoreBuilder`. Both accept the same connection options as before, so code that chains options and passes the result to the SDK needs no change:

```go
    // unchanged in version 4
    var config ld.Config
    config.DataStore = ldcomponents.PersistentDataStore(
        ldredis.DataStore().URL("redis://my-redis-host").Prefix("my-prefix"),
    )
    config.BigSegments = ldcomponents.BigSegments(
        ldredis.BigSegmentStore().URL("redis://my-redis-host"),
    )
```

Only code that names the builder type has to change:

```go
// before
func configureStore(b *ldredis.StoreBuilder[subsystems.PersistentDataStore]) { ... }

// after
func configureStore(b *ldredis.DataStoreBuilder) { ... }
```

**3. Updates use a Lua script by default.** This requires permission to run scripts on the Redis server, as described in [Requirements](#requirements), and there is no automatic fallback. If you cannot grant that permission, ask for the version 3 behavior explicitly:

```go
    var config ld.Config
    config.DataStore = ldcomponents.PersistentDataStore(
        ldredis.DataStore().UpsertMode(ldredis.UpsertModeWatch),
    )
```

Nothing about the Redis keys or the stored data changes in version 4, in either mode. Version 4 reads and writes exactly what version 3 did, so it interoperates with the Relay Proxy and with SDKs in other languages that share the database, and you can roll it out one instance at a time.

## LaunchDarkly overview

[LaunchDarkly](https://www.launchdarkly.com) is a feature management platform that serves trillions of feature flags daily to help teams build better software, faster. [Get started](https://docs.launchdarkly.com/docs/getting-started) using LaunchDarkly today!

## About LaunchDarkly

* LaunchDarkly is a continuous delivery platform that provides feature flags as a service and allows developers to iterate quickly and safely. We allow you to easily flag your features and manage them from the LaunchDarkly dashboard.  With LaunchDarkly, you can:
    * Roll out a new feature to a subset of your users (like a group of users who opt-in to a beta tester group), gathering feedback and bug reports from real-world use cases.
    * Gradually roll out a feature to an increasing percentage of users, and track the effect that the feature has on key metrics (for instance, how likely is a user to complete a purchase if they have feature A versus feature B?).
    * Turn off a feature that you realize is causing performance problems in production, without needing to re-deploy, or even restart the application with a changed configuration file.
    * Grant access to certain features based on user attributes, like payment plan (eg: users on the ‘gold’ plan get access to more features than users in the ‘silver’ plan). Disable parts of your application to facilitate maintenance, without taking everything offline.
* LaunchDarkly provides feature flag SDKs for a wide variety of languages and technologies. Read [our documentation](https://docs.launchdarkly.com/docs) for a complete list.
* Explore LaunchDarkly
    * [launchdarkly.com](https://www.launchdarkly.com/ "LaunchDarkly Main Website") for more information
    * [docs.launchdarkly.com](https://docs.launchdarkly.com/  "LaunchDarkly Documentation") for our documentation and SDK reference guides
    * [apidocs.launchdarkly.com](https://apidocs.launchdarkly.com/  "LaunchDarkly API Documentation") for our API documentation
    * [blog.launchdarkly.com](https://blog.launchdarkly.com/  "LaunchDarkly Blog Documentation") for the latest product updates
