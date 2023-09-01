module github.com/launchdarkly/go-server-sdk-redis-redigo/v2

go 1.18

require (
	github.com/gomodule/redigo v1.8.2
	github.com/launchdarkly/go-sdk-common/v3 v3.0.1
	github.com/launchdarkly/go-server-sdk/v7 v7.0.0
	github.com/stretchr/testify v1.7.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/uuid v1.1.1 // indirect
	github.com/gregjones/httpcache v0.0.0-20171119193500-2bcd89a1743f // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/launchdarkly/ccache v1.1.0 // indirect
	github.com/launchdarkly/eventsource v1.6.2 // indirect
	github.com/launchdarkly/go-jsonstream/v3 v3.0.0 // indirect
	github.com/launchdarkly/go-sdk-events/v3 v3.0.0 // indirect
	github.com/launchdarkly/go-semver v1.0.2 // indirect
	github.com/launchdarkly/go-server-sdk-evaluation/v3 v3.0.0 // indirect
	github.com/launchdarkly/go-test-helpers/v2 v2.3.1 // indirect
	github.com/launchdarkly/go-test-helpers/v3 v3.0.2 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/exp v0.0.0-20220823124025-807a23277127 // indirect
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
)

replace github.com/launchdarkly/go-sdk-common/v3 => github.com/launchdarkly/go-sdk-common-private/v3 v3.0.0-alpha.6.0.20230829225529-e3a87e3952ac

replace github.com/launchdarkly/go-server-sdk/v7 => github.com/launchdarkly/go-server-sdk-private/v7 v7.0.0-20230831202925-f824718cfcca

replace github.com/launchdarkly/go-server-sdk-evaluation/v3 => github.com/launchdarkly/go-server-sdk-evaluation-private/v3 v3.0.0-20230829233102-4fc0fa5a3369

replace github.com/launchdarkly/go-sdk-events/v3 => github.com/launchdarkly/go-sdk-events-private/v3 v3.0.0-20230829233031-ed3dc538caac
