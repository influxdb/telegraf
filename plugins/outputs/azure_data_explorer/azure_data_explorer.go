//go:generate ../../../tools/readme_config_includer/generator
package azure_data_explorer

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-kusto-go/kusto"
	kustoerrors "github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/Azure/azure-kusto-go/kusto/kql"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/outputs"
	"github.com/influxdata/telegraf/plugins/serializers/json"
)

//go:embed sample.conf
var sampleConfig string

type AzureDataExplorer struct {
	Endpoint        string          `toml:"endpoint_url"`
	Database        string          `toml:"database"`
	Log             telegraf.Logger `toml:"-"`
	Timeout         config.Duration `toml:"timeout"`
	MetricsGrouping string          `toml:"metrics_grouping_type"`
	TableName       string          `toml:"table_name"`
	CreateTables    bool            `toml:"create_tables"`
	IngestionType   string          `toml:"ingestion_type"`
	serializer      telegraf.Serializer
	kustoClient     *kusto.Client
	metricIngestors map[string]ingest.Ingestor
}

const (
	tablePerMetric = "tablepermetric"
	singleTable    = "singletable"
	// These control the amount of memory we use when ingesting blobs
	bufferSize = 1 << 20 // 1 MiB
	maxBuffers = 5
)

const managedIngestion = "managed"
const queuedIngestion = "queued"

func (*AzureDataExplorer) SampleConfig() string {
	return sampleConfig
}

// Initialize the client and the ingestor
func (adx *AzureDataExplorer) Connect() error {
	conn := kusto.NewConnectionStringBuilder(adx.Endpoint).WithDefaultAzureCredential()
	// Since init is called before connect, we can set the connector details here including the type. This will be used for telemetry and tracing.
	conn.SetConnectorDetails("Telegraf", internal.ProductToken(), "", "", false, "")
	client, err := kusto.New(conn)
	if err != nil {
		return err
	}
	adx.kustoClient = client
	adx.metricIngestors = make(map[string]ingest.Ingestor)

	return nil
}

// Clean up and close the ingestor
func (adx *AzureDataExplorer) Close() error {
	var errs []error
	for _, v := range adx.metricIngestors {
		if err := v.Close(); err != nil {
			// accumulate errors while closing ingestors
			errs = append(errs, err)
		}
	}
	if err := adx.kustoClient.Close(); err != nil {
		errs = append(errs, err)
	}

	adx.kustoClient = nil
	adx.metricIngestors = nil

	if len(errs) == 0 {
		adx.Log.Info("Closed ingestors and client")
		return nil
	}
	// Combine errors into a single object and return the combined error
	return kustoerrors.GetCombinedError(errs...)
}

func (adx *AzureDataExplorer) Write(metrics []telegraf.Metric) error {
	if adx.MetricsGrouping == tablePerMetric {
		return adx.writeTablePerMetric(metrics)
	}
	return adx.writeSingleTable(metrics)
}

func (adx *AzureDataExplorer) writeTablePerMetric(metrics []telegraf.Metric) error {
	tableMetricGroups := make(map[string][]byte)
	// Group metrics by name and serialize them
	for _, m := range metrics {
		tableName := m.Name()
		metricInBytes, err := adx.serializer.Serialize(m)
		if err != nil {
			return err
		}
		if existingBytes, ok := tableMetricGroups[tableName]; ok {
			tableMetricGroups[tableName] = append(existingBytes, metricInBytes...)
		} else {
			tableMetricGroups[tableName] = metricInBytes
		}
	}
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(adx.Timeout))
	defer cancel()

	// Push the metrics for each table
	format := ingest.FileFormat(ingest.JSON)
	for tableName, tableMetrics := range tableMetricGroups {
		if err := adx.pushMetrics(ctx, format, tableName, tableMetrics); err != nil {
			return err
		}
	}

	return nil
}

func (adx *AzureDataExplorer) writeSingleTable(metrics []telegraf.Metric) error {
	// serialise each metric in metrics - store in byte[]
	metricsArray := make([]byte, 0)
	for _, m := range metrics {
		metricsInBytes, err := adx.serializer.Serialize(m)
		if err != nil {
			return err
		}
		metricsArray = append(metricsArray, metricsInBytes...)
	}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(adx.Timeout))
	defer cancel()

	// push metrics to a single table
	format := ingest.FileFormat(ingest.JSON)
	err := adx.pushMetrics(ctx, format, adx.TableName, metricsArray)
	return err
}

func (adx *AzureDataExplorer) pushMetrics(ctx context.Context, format ingest.FileOption, tableName string, metricsArray []byte) error {
	var metricIngestor ingest.Ingestor
	var err error

	metricIngestor, err = adx.getMetricIngestor(ctx, tableName)
	if err != nil {
		return err
	}

	length := len(metricsArray)
	adx.Log.Debugf("Writing %d metrics to table %q", length, tableName)
	reader := bytes.NewReader(metricsArray)
	mapping := ingest.IngestionMappingRef(tableName+"_mapping", ingest.JSON)
	if metricIngestor != nil {
		if _, err := metricIngestor.FromReader(ctx, reader, format, mapping); err != nil {
			adx.Log.Errorf("sending ingestion request to Azure Data Explorer for table %q failed: %v", tableName, err)
		}
	}
	return nil
}

func (adx *AzureDataExplorer) getMetricIngestor(ctx context.Context, tableName string) (ingest.Ingestor, error) {
	ingestor := adx.metricIngestors[tableName]

	if ingestor == nil {
		if err := adx.createAzureDataExplorerTable(ctx, tableName); err != nil {
			return nil, fmt.Errorf("creating table for %q failed: %w", tableName, err)
		}
		// create a new ingestor client for the table
		tempIngestor, err := createIngestorByTable(adx.kustoClient, adx.Database, tableName, adx.IngestionType)
		if err != nil {
			return nil, fmt.Errorf("creating ingestor for %q failed: %w", tableName, err)
		}
		adx.metricIngestors[tableName] = tempIngestor
		adx.Log.Debugf("Ingestor for table %s created", tableName)
		ingestor = tempIngestor
	}
	return ingestor, nil
}

func (adx *AzureDataExplorer) createAzureDataExplorerTable(ctx context.Context, tableName string) error {
	if !adx.CreateTables {
		adx.Log.Info("skipped table creation")
		return nil
	}

	if _, err := adx.kustoClient.Mgmt(ctx, adx.Database, createTableCommand(tableName)); err != nil {
		return err
	}

	if _, err := adx.kustoClient.Mgmt(ctx, adx.Database, createTableMappingCommand(tableName)); err != nil {
		return err
	}

	return nil
}

func (adx *AzureDataExplorer) Init() error {
	if adx.Endpoint == "" {
		return errors.New("endpoint configuration cannot be empty")
	}
	if adx.Database == "" {
		return errors.New("database configuration cannot be empty")
	}

	adx.MetricsGrouping = strings.ToLower(adx.MetricsGrouping)
	if adx.MetricsGrouping == singleTable && adx.TableName == "" {
		return errors.New("table name cannot be empty for SingleTable metrics grouping type")
	}
	if adx.MetricsGrouping == "" {
		adx.MetricsGrouping = tablePerMetric
	}
	if !(adx.MetricsGrouping == singleTable || adx.MetricsGrouping == tablePerMetric) {
		return errors.New("metrics grouping type is not valid")
	}

	if adx.IngestionType == "" {
		adx.IngestionType = queuedIngestion
	} else if !(choice.Contains(adx.IngestionType, []string{managedIngestion, queuedIngestion})) {
		return fmt.Errorf("unknown ingestion type %q", adx.IngestionType)
	}

	serializer := &json.Serializer{
		TimestampUnits:  config.Duration(time.Nanosecond),
		TimestampFormat: time.RFC3339Nano,
	}
	if err := serializer.Init(); err != nil {
		return err
	}
	adx.serializer = serializer
	return nil
}

func init() {
	outputs.Add("azure_data_explorer", func() telegraf.Output {
		return &AzureDataExplorer{
			Timeout:      config.Duration(20 * time.Second),
			CreateTables: true,
		}
	})
}

// For each table create the ingestor
func createIngestorByTable(client *kusto.Client, database, tableName, ingestionType string) (ingest.Ingestor, error) {
	switch strings.ToLower(ingestionType) {
	case managedIngestion:
		mi, err := ingest.NewManaged(client, database, tableName)
		return mi, err
	case queuedIngestion:
		qi, err := ingest.New(client, database, tableName, ingest.WithStaticBuffer(bufferSize, maxBuffers))
		return qi, err
	}
	return nil, fmt.Errorf(`ingestion_type has to be one of %q or %q`, managedIngestion, queuedIngestion)
}

func createTableCommand(table string) kusto.Statement {
	builder := kql.New(`.create-merge table ['`).AddTable(table).AddLiteral(`'] `)
	builder.AddLiteral(`(['fields']:dynamic, ['name']:string, ['tags']:dynamic, ['timestamp']:datetime);`)

	return builder
}

func createTableMappingCommand(table string) kusto.Statement {
	builder := kql.New(`.create-or-alter table ['`).AddTable(table).AddLiteral(`'] `)
	builder.AddLiteral(`ingestion json mapping '`).AddTable(table + "_mapping").AddLiteral(`' `)
	builder.AddLiteral(`'[{"column":"fields", `)
	builder.AddLiteral(`"Properties":{"Path":"$[\'fields\']"}},{"column":"name", `)
	builder.AddLiteral(`"Properties":{"Path":"$[\'name\']"}},{"column":"tags", `)
	builder.AddLiteral(`"Properties":{"Path":"$[\'tags\']"}},{"column":"timestamp", `)
	builder.AddLiteral(`"Properties":{"Path":"$[\'timestamp\']"}}]'`)

	return builder
}
