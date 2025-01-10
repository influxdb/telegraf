//go:generate ../../../tools/readme_config_includer/generator
package opentelemetry

import (
	_ "embed"
	"fmt"
	"net"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	pprofileotlp "go.opentelemetry.io/proto/otlp/collector/profiles/v1experimental"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/influxdata/influxdb-observability/otel2influx"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type OpenTelemetry struct {
	ServiceAddress      string          `toml:"service_address"`
	SpanDimensions      []string        `toml:"span_dimensions"`
	LogRecordDimensions []string        `toml:"log_record_dimensions"`
	ProfileDimensions   []string        `toml:"profile_dimensions"`
	MetricsSchema       string          `toml:"metrics_schema"`
	MaxMsgSize          config.Size     `toml:"max_msg_size"`
	Timeout             config.Duration `toml:"timeout"`
	Log                 telegraf.Logger `toml:"-"`
	tls.ServerConfig

	listener   net.Listener // overridden in tests
	grpcServer *grpc.Server

	wg sync.WaitGroup
}

func (*OpenTelemetry) SampleConfig() string {
	return sampleConfig
}

func (o *OpenTelemetry) Init() error {
	if o.ServiceAddress == "" {
		o.ServiceAddress = "0.0.0.0:4317"
	}
	switch o.MetricsSchema {
	case "": // Set default
		o.MetricsSchema = "prometheus-v1"
	case "prometheus-v1", "prometheus-v2": // Valid values
	default:
		return fmt.Errorf("invalid metric schema %q", o.MetricsSchema)
	}

	return nil
}

func (o *OpenTelemetry) Start(acc telegraf.Accumulator) error {
	var grpcOptions []grpc.ServerOption
	if tlsConfig, err := o.ServerConfig.TLSConfig(); err != nil {
		return err
	} else if tlsConfig != nil {
		grpcOptions = append(grpcOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}
	if o.Timeout > 0 {
		grpcOptions = append(grpcOptions, grpc.ConnectionTimeout(time.Duration(o.Timeout)))
	}
	if o.MaxMsgSize > 0 {
		grpcOptions = append(grpcOptions, grpc.MaxRecvMsgSize(int(o.MaxMsgSize)))
	}

	logger := &otelLogger{o.Log}
	influxWriter := &writeToAccumulator{acc}
	o.grpcServer = grpc.NewServer(grpcOptions...)

	traceSvc, err := newTraceService(logger, influxWriter, o.SpanDimensions)
	if err != nil {
		return err
	}
	ptraceotlp.RegisterGRPCServer(o.grpcServer, traceSvc)

	metricsSvc, err := newMetricsService(logger, influxWriter, o.MetricsSchema)
	if err != nil {
		return err
	}
	pmetricotlp.RegisterGRPCServer(o.grpcServer, metricsSvc)

	logsSvc, err := newLogsService(logger, influxWriter, o.LogRecordDimensions)
	if err != nil {
		return err
	}
	plogotlp.RegisterGRPCServer(o.grpcServer, logsSvc)

	profileSvc, err := newProfileService(acc, o.Log, o.ProfileDimensions)
	if err != nil {
		return err
	}
	pprofileotlp.RegisterProfilesServiceServer(o.grpcServer, profileSvc)

	o.listener, err = net.Listen("tcp", o.ServiceAddress)
	if err != nil {
		return err
	}

	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		if err := o.grpcServer.Serve(o.listener); err != nil {
			acc.AddError(fmt.Errorf("failed to stop OpenTelemetry gRPC service: %w", err))
		}
	}()

	return nil
}

func (*OpenTelemetry) Gather(telegraf.Accumulator) error {
	return nil
}

func (o *OpenTelemetry) Stop() {
	if o.grpcServer != nil {
		o.grpcServer.Stop()
	}
	o.listener = nil

	o.wg.Wait()
}

func init() {
	inputs.Add("opentelemetry", func() telegraf.Input {
		return &OpenTelemetry{
			SpanDimensions:      otel2influx.DefaultOtelTracesToLineProtocolConfig().SpanDimensions,
			LogRecordDimensions: otel2influx.DefaultOtelLogsToLineProtocolConfig().LogRecordDimensions,
			Timeout:             config.Duration(5 * time.Second),
		}
	})
}
