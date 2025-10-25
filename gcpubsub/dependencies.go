package gcpubsub

import (
	"context"
	"io"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Dependencies 汇总可选协作者，用于注入日志、指标、追踪与时间源。
type Dependencies struct {
	Logger          log.Logger
	Meter           metric.Meter
	Tracer          trace.Tracer
	Clock           func() time.Time
	CredentialsJSON []byte
	Dial            DialOptions
	ClientFactory   ClientFactory
}

// DialOptions 描述 gRPC 连接参数。
type DialOptions struct {
	GRPCConnPoolSize int
	Insecure         bool
}

// Credentials 描述客户端认证与连接设置。
type Credentials struct {
	JSON                  []byte
	EmulatorEndpoint      string
	UseApplicationDefault bool
}

// ClientFactory 创建 Pub/Sub 客户端的函数签名。
type ClientFactory func(ctx context.Context, projectID string, creds Credentials, dial DialOptions) (*pubsub.Client, error)

type resolvedDependencies struct {
	logger      log.Logger
	meter       metric.Meter
	tracer      trace.Tracer
	clock       func() time.Time
	dial        DialOptions
	factory     ClientFactory
	credentials Credentials
}

func resolveDependencies(cfg Config, deps Dependencies) resolvedDependencies {
	logger := deps.Logger
	if logger == nil {
		logger = log.NewStdLogger(io.Discard)
	}

	meter := deps.Meter
	if meter == nil {
		meter = otel.GetMeterProvider().Meter(cfg.MeterName)
	}

	tracer := deps.Tracer
	if tracer == nil {
		tracer = otel.Tracer(cfg.MeterName)
	}

	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}

	dial := deps.Dial
	if dial.GRPCConnPoolSize <= 0 {
		dial.GRPCConnPoolSize = 4
	}
	if cfg.EmulatorEndpoint != "" {
		dial.Insecure = true
	}

	factory := deps.ClientFactory
	if factory == nil {
		factory = defaultClientFactory
	}

	creds := Credentials{
		EmulatorEndpoint: cfg.EmulatorEndpoint,
	}
	if len(deps.CredentialsJSON) > 0 {
		creds.JSON = cloneBytes(deps.CredentialsJSON)
	} else if cfg.EmulatorEndpoint == "" {
		creds.UseApplicationDefault = true
	}

	return resolvedDependencies{
		logger:      logger,
		meter:       meter,
		tracer:      tracer,
		clock:       clock,
		dial:        dial,
		factory:     factory,
		credentials: creds,
	}
}

func defaultClientFactory(ctx context.Context, projectID string, creds Credentials, dial DialOptions) (*pubsub.Client, error) {
	opts := make([]option.ClientOption, 0, 4)

	if creds.EmulatorEndpoint != "" {
		opts = append(opts,
			option.WithEndpoint(creds.EmulatorEndpoint),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	} else if len(creds.JSON) > 0 {
		opts = append(opts, option.WithCredentialsJSON(creds.JSON))
	}

	if dial.GRPCConnPoolSize > 0 {
		opts = append(opts, option.WithGRPCConnectionPool(dial.GRPCConnPoolSize))
	}
	if dial.Insecure && creds.EmulatorEndpoint == "" {
		opts = append(opts, option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	}

	return pubsub.NewClient(ctx, projectID, opts...)
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}
