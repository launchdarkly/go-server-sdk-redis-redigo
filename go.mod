module github.com/launchdarkly/go-server-sdk-redis-redigo/v3

go 1.24.0

require (
	github.com/gomodule/redigo v1.8.2
	github.com/launchdarkly/go-sdk-common/v3 v3.5.0
	github.com/launchdarkly/go-server-sdk/v7 v7.15.3-0.20260610190543-b30d70b4623f
	github.com/stretchr/testify v1.9.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/uuid v1.1.1 // indirect
	github.com/gregjones/httpcache v0.0.0-20171119193500-2bcd89a1743f // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/launchdarkly/ccache v1.1.0 // indirect
	github.com/launchdarkly/eventsource v1.10.0 // indirect
	github.com/launchdarkly/go-jsonstream/v3 v3.1.1 // indirect
	github.com/launchdarkly/go-sdk-events/v3 v3.6.2-0.20260610185926-04050b02df99 // indirect
	github.com/launchdarkly/go-semver v1.0.3 // indirect
	github.com/launchdarkly/go-server-sdk-evaluation/v3 v3.0.1 // indirect
	github.com/launchdarkly/go-test-helpers/v3 v3.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// v3.0.3 upgraded to the go-sdk-common/v4 (and related /v4) core libraries. Those
// /v4 major bumps are a breaking change for customers (Go semantic import
// versioning), so v3.0.3 is retracted in favor of a v3-core release. See SDK-2496.
retract v3.0.3
