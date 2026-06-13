module github.com/hermes-notifications/hermes

go 1.26

require (
	github.com/DataDog/dd-trace-go/contrib/aws/aws-sdk-go-v2/v2 v2.7.0
	github.com/DataDog/dd-trace-go/contrib/database/sql/v2 v2.7.0
	github.com/DataDog/dd-trace-go/contrib/go-chi/chi.v5/v2 v2.7.0
	github.com/DataDog/dd-trace-go/contrib/jackc/pgx.v5/v2 v2.7.0
	github.com/DataDog/dd-trace-go/contrib/log/slog/v2 v2.7.0
	github.com/DataDog/dd-trace-go/contrib/net/http/v2 v2.7.0
	github.com/DataDog/dd-trace-go/contrib/redis/go-redis.v9/v2 v2.7.0
	github.com/DataDog/orchestrion v1.8.0
	github.com/aws/aws-sdk-go-v2 v1.41.5
	github.com/aws/aws-sdk-go-v2/config v1.32.12
	github.com/aws/aws-sdk-go-v2/credentials v1.19.12
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.43.4
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.41.4
	github.com/aws/aws-sdk-go-v2/service/sesv2 v1.60.1
	github.com/centrifugal/centrifuge-go v0.10.12
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/danielgtaylor/huma/v2 v2.37.2
	github.com/exaring/otelpgx v0.10.0
	github.com/fatih/color v1.19.0
	github.com/go-chi/chi/v5 v5.2.5
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/golang-migrate/migrate/v4 v4.19.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.8.0
	github.com/nats-io/nats.go v1.49.0
	github.com/redis/go-redis/extra/redisotel/v9 v9.18.0
	github.com/redis/go-redis/v9 v9.18.0
	github.com/spf13/cobra v1.10.2
	github.com/wneessen/go-mail v0.7.2
	go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws v0.62.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.62.0
	go.opentelemetry.io/contrib/instrumentation/runtime v0.62.0
	go.opentelemetry.io/otel v1.42.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.40.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.42.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.42.0
	go.opentelemetry.io/otel/metric v1.42.0
	go.opentelemetry.io/otel/sdk v1.42.0
	go.opentelemetry.io/otel/sdk/metric v1.42.0
	go.opentelemetry.io/otel/trace v1.42.0
	golang.org/x/crypto v0.49.0
	golang.org/x/text v0.35.0
)

require github.com/sergi/go-diff v1.3.1 // indirect

require (
	github.com/DataDog/datadog-agent/comp/core/tagger/origindetection v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/obfuscate v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/opentelemetry-mapping-go/otlp/attributes v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/proto v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/remoteconfig/state v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/template v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/trace v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/util/log v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/util/scrubber v0.75.2 // indirect
	github.com/DataDog/datadog-agent/pkg/version v0.75.2 // indirect
	github.com/DataDog/datadog-go/v5 v5.8.2 // indirect
	github.com/DataDog/dd-trace-go/v2 v2.7.0 // indirect
	github.com/DataDog/go-libddwaf/v4 v4.8.0 // indirect
	github.com/DataDog/go-runtime-metrics-internal v0.0.4-0.20260217080614-b0f4edc38a6d // indirect
	github.com/DataDog/go-sqllexer v0.1.10 // indirect
	github.com/DataDog/go-tuf v1.1.1-0.5.2 // indirect
	github.com/DataDog/sketches-go v1.4.7 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.6.0-default-no-op // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.8 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.20 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/eventbridge v1.30.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.3.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.10.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.20 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.17.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/kinesis v1.43.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.53.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sfn v1.26.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/sns v1.34.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sqs v1.38.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.9 // indirect
	github.com/aws/smithy-go v1.24.2 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/blakesmith/ar v0.0.0-20190502131153-809d4375e1fb // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/centrifugal/protocol v0.16.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.3.2 // indirect
	github.com/charmbracelet/x/ansi v0.10.2 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/cihub/seelog v0.0.0-20170130134532-f561c5e57575 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/dave/dst v0.27.3 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.9.1 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/pprof v0.0.0-20260302011040-a15ffb7f9dcc // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/hashicorp/go-version v1.8.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/lib/pq v1.11.1 // indirect
	github.com/linkdata/deadlock v0.5.5 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20251013123823-9fd1530e3ec3 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.20 // indirect
	github.com/minio/highwayhash v1.0.4-0.20251030100505-070ab1a87a76 // indirect
	github.com/minio/simdjson-go v0.4.5 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/nats-io/jwt/v2 v2.8.1 // indirect
	github.com/nats-io/nats-server/v2 v2.12.6 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/outcaste-io/ristretto v0.2.3 // indirect
	github.com/petermattis/goid v0.0.0-20250813065127-a731cc31b4fe // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/polyfloyd/go-errorlint v1.8.1-0.20250906200200-9b25878c4dea // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/puzpuzpuz/xsync/v3 v3.5.1 // indirect
	github.com/redis/go-redis/extra/rediscmd/v9 v9.18.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/secure-systems-lab/go-securesystemslib v0.9.1 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/segmentio/encoding v0.4.0 // indirect
	github.com/shadowspore/fossil-delta v0.0.0-20241213113458-1d797d70cbe3 // indirect
	github.com/shirou/gopsutil/v4 v4.25.11 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tinylib/msgp v1.6.1 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/trailofbits/go-mutexasserts v0.0.0-20250514102930-c1f3d2e37561 // indirect
	github.com/urfave/cli/v2 v2.27.7 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/xrash/smetrics v0.0.0-20250705151800-55b8f293f342 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/collector/component v1.54.0 // indirect
	go.opentelemetry.io/collector/featuregate v1.54.0 // indirect
	go.opentelemetry.io/collector/pdata v1.54.0 // indirect
	go.opentelemetry.io/collector/pdata/pprofile v0.142.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/term v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.42.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260311181403-84a4fc48630c // indirect
	google.golang.org/grpc v1.79.3 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/ini.v1 v1.67.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/DataDog/dd-trace-go/v2 => github.com/DataDog/dd-trace-go/v2 v2.7.0
