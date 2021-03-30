module github.com/launchdarkly/go-server-sdk-redis-redigo

go 1.13

require (
	github.com/gomodule/redigo v1.8.2
	github.com/stretchr/testify v1.6.1
	gopkg.in/launchdarkly/go-sdk-common.v2 v2.3.0
	gopkg.in/launchdarkly/go-server-sdk.v5 v5.3.0
)

replace gopkg.in/launchdarkly/go-sdk-common.v2 => github.com/launchdarkly/go-sdk-common-private/v2 v2.2.3-0.20210323175925-2f53ef23e94c

replace gopkg.in/launchdarkly/go-server-sdk-evaluation.v1 => github.com/launchdarkly/go-server-sdk-evaluation-private v1.2.1-0.20210323201644-112b8c0df0c7

replace gopkg.in/launchdarkly/go-server-sdk.v5 => github.com/launchdarkly/go-server-sdk-private/v5 v5.2.2-0.20210323221017-de150cb8acdc
