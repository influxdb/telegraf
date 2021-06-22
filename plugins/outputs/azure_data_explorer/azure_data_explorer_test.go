package azure_data_explorer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/ingest"
	"github.com/influxdata/telegraf/plugins/serializers"
	"github.com/influxdata/telegraf/testutil"
)

var logger testutil.Logger = testutil.Logger{}
var actualOutputMetric map[string]interface{}
var queriesSentToAzureDataExplorer = make([]string, 0)

func TestWrite(t *testing.T) {
	azureDataExplorerOutput := AzureDataExplorer{
		Endpoint:       "",
		Database:       "",
		ClientId:       "",
		ClientSecret:   "",
		TenantId:       "",
		DataFormat:     "",
		Log:            logger,
		Client:         &kusto.Client{},
		Ingesters:      map[string]localIngestor{},
		Serializer:     nil,
		CreateIngestor: createFakeIngestor,
		CreateClient:   createFakeClient,
	}

	azureDataExplorerOutput.Connect()
	serializerJson, _ := serializers.NewJSONSerializer(time.Second)
	azureDataExplorerOutput.SetSerializer(serializerJson)
	azureDataExplorerOutput.Write(testutil.MockMetrics())

	expectedNameOfMetric := "test1"
	if actualOutputMetric["name"] != expectedNameOfMetric {
		t.Errorf("Error in Write: expected %s, but actual %s", expectedNameOfMetric, actualOutputMetric["name"])
	}

	createTableString := fmt.Sprintf(createTableCommand, expectedNameOfMetric)
	if queriesSentToAzureDataExplorer[0] != createTableString {
		t.Errorf("Error in Write: expected create table query is %s, but actual is %s", queriesSentToAzureDataExplorer[0], createTableString)
	}

	fields := actualOutputMetric["fields"]
	logger.Debug((fields.(map[string]interface{}))["value"])

}

func createFakeIngestor(client localClient, database string, namespace string) (localIngestor, error) {
	return &fakeIngestor{}, nil
}

func createFakeClient(endpoint string, clientId string, clientSecret string, tenantId string) (localClient, error) {
	return &fakeClient{}, nil
}

type fakeClient struct {
}

func (f *fakeClient) Mgmt(ctx context.Context, db string, query kusto.Stmt, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	queriesSentToAzureDataExplorer = append(queriesSentToAzureDataExplorer, query.String())
	return &kusto.RowIterator{}, nil
}

type fakeIngestor struct {
}

func (f *fakeIngestor) FromReader(ctx context.Context, reader io.Reader, options ...ingest.FileOption) (*ingest.Result, error) {

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	firstLine := scanner.Text()
	err := json.Unmarshal([]byte(firstLine), &actualOutputMetric)
	if err != nil {
		logger.Errorf(err.Error())
	}
	return &ingest.Result{}, nil
}
