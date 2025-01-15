package eh_commons

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	eventhub "github.com/Azure/azure-event-hubs-go/v3"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/serializers/json"
	"github.com/influxdata/telegraf/testutil"
)

/*
** Wrapper interface mock for eventhub.Hub
 */

type mockEventHub struct {
	mock.Mock
}

func (eh *mockEventHub) GetHub(s string) error {
	args := eh.Called(s)
	return args.Error(0)
}

func (eh *mockEventHub) Close(ctx context.Context) error {
	args := eh.Called(ctx)
	return args.Error(0)
}

func (eh *mockEventHub) SendBatch(ctx context.Context, iterator eventhub.BatchIterator, opts ...eventhub.BatchOption) error {
	args := eh.Called(ctx, iterator, opts)
	return args.Error(0)
}

/* End wrapper interface */

func TestInitAndWrite(t *testing.T) {
	serializer := &json.Serializer{}
	require.NoError(t, serializer.Init())

	mockHub := &mockEventHub{}
	e := &EventHubs{
		Hub:              mockHub,
		ConnectionString: "mock",
		Timeout:          config.Duration(time.Second * 5),
		MaxMessageSize:   1000000,
	}
	e.SetSerializer(serializer)
	mockHub.On("GetHub", mock.Anything).Return(nil).Once()
	require.NoError(t, e.Init())
	mockHub.AssertExpectations(t)

	metrics := testutil.MockMetrics()

	mockHub.On("SendBatch", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	require.NoError(t, e.Write(metrics))
	mockHub.AssertExpectations(t)
}

/*
** Integration test (requires an Event Hubs instance)
 */

func TestInitAndWriteIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if os.Getenv("EVENTHUB_CONNECTION_STRING") == "" {
		t.Skip("Missing environment variable EVENTHUB_CONNECTION_STRING")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	// Create a new, empty Event Hub
	// NB: for this to work, the connection string needs to grant "Manage" permissions on the root namespace
	mHub, err := eventhub.NewHubManagerFromConnectionString(os.Getenv("EVENTHUB_CONNECTION_STRING"))
	require.NoError(t, err)

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	name := fmt.Sprintf("testmetrics%05d", r.Intn(10000))

	entity, err := mHub.Put(ctx, name, eventhub.HubWithPartitionCount(1))
	require.NoError(t, err)

	// Delete the test hub
	defer func() {
		err := mHub.Delete(ctx, entity.Name)
		require.NoError(t, err)
	}()

	testHubCS := os.Getenv("EVENTHUB_CONNECTION_STRING") + ";EntityPath=" + entity.Name

	// Configure the plugin to target the newly created hub
	serializer := &json.Serializer{}
	require.NoError(t, serializer.Init())
	e := &EventHubs{
		Hub:              &EventHub{},
		ConnectionString: testHubCS,
		Timeout:          config.Duration(time.Second * 5),
	}

	e.SetSerializer(serializer)

	// Verify that we can connect to Event Hubs
	require.NoError(t, e.Init())

	// Verify that we can successfully write data to Event Hubs
	metrics := testutil.MockMetrics()
	require.NoError(t, e.Write(metrics))
	e.Close()

	/*
	** Verify we can read data back from the test hub
	 */

	exit := make(chan string)

	// Create a hub client for receiving
	hub, err := eventhub.NewHubFromConnectionString(testHubCS)
	require.NoError(t, err)

	// The handler function will pass received messages via the channel
	handler := func(_ context.Context, event *eventhub.Event) error {
		exit <- string(event.Data)
		return nil
	}

	// Set up the receivers
	runtimeInfo, err := hub.GetRuntimeInformation(ctx)
	require.NoError(t, err)

	for _, partitionID := range runtimeInfo.PartitionIDs {
		_, err := hub.Receive(ctx, partitionID, handler, eventhub.ReceiveWithStartingOffset("-1"))
		require.NoError(t, err)
	}

	// Wait to receive the same number of messages sent, with timeout
	received := 0
wait:
	for _, metric := range metrics {
		select {
		case m := <-exit:
			t.Logf("Received for %s: %s", metric.Name(), m)
			received = received + 1
		case <-time.After(10 * time.Second):
			t.Logf("Timeout")
			break wait
		}
	}

	// Make sure received == sent
	require.Len(t, metrics, received)
}
