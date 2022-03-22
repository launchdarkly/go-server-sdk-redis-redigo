module github.com/launchdarkly/go-server-sdk-redis-redigo/v2

go 1.16

require (
	github.com/gomodule/redigo v1.8.2
	github.com/launchdarkly/go-sdk-common/v3 v3.0.0
	github.com/launchdarkly/go-server-sdk/v6 v6.0.0
	github.com/stretchr/testify v1.6.1
)

replace github.com/launchdarkly/go-sdk-common/v3 => github.com/launchdarkly/go-sdk-common-private/v3 v3.0.0-alpha.5

replace github.com/launchdarkly/go-sdk-events/v2 => github.com/launchdarkly/go-sdk-events-private/v2 v2.0.0-alpha.5

replace github.com/launchdarkly/go-server-sdk-evaluation/v2 => github.com/launchdarkly/go-server-sdk-evaluation-private/v2 v2.0.0-alpha.7

replace github.com/launchdarkly/go-server-sdk/v6 => github.com/launchdarkly/go-server-sdk-private/v6 v6.0.0-alpha.2
