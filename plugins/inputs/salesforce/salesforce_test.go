package salesforce

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func Test_Gather(t *testing.T) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		if _, err := w.Write([]byte(testJSON)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			t.Error(err)
			return
		}
	}))
	defer fakeServer.Close()

	plugin := newSalesforce()
	plugin.sessionID = "test_session"
	u, err := url.Parse(fakeServer.URL)
	if err != nil {
		t.Error(err)
	}
	plugin.serverURL = u

	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(plugin.Gather))

	require.Len(t, acc.Metrics, 1)
	m := acc.Metrics[0]
	require.Len(t, m.Fields, 46)
	require.Len(t, m.Tags, 2)
}

var testJSON = `{
  "ConcurrentAsyncGetReportInstances" : {
    "Max" : 200,
    "Remaining" : 200
  },
  "ConcurrentSyncReportRuns" : {
    "Max" : 20,
    "Remaining" : 20
  },
  "DailyApiRequests" : {
    "Max" : 25000,
    "Remaining" : 24926,
    "AgilePoint" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Ant Migration Tool" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Axsy Server Integration" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Chatter Desktop" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Chatter Mobile for BlackBerry" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Dataloader Bulk" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Dataloader Partner" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "EAHelperBot" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Force.com IDE" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "LiveText for Salesforce" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "LiveText for Salesforce (QA)" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "MyU App" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SMS Magic Interact" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Chatter" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Files" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Mobile Dashboards" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Social Customer Service (SCS)" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Touch" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce for Outlook" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce1 for Android" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce1 for iOS" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SalesforceA" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SalesforceIQ" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Workbench" : {
      "Max" : 0,
      "Remaining" : 0
    }
  },
  "DailyAsyncApexExecutions" : {
    "Max" : 250000,
    "Remaining" : 250000
  },
  "DailyBulkApiRequests" : {
    "Max" : 10000,
    "Remaining" : 10000,
    "AgilePoint" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Ant Migration Tool" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Axsy Server Integration" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Chatter Desktop" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Chatter Mobile for BlackBerry" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Dataloader Bulk" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Dataloader Partner" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "EAHelperBot" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Force.com IDE" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "LiveText for Salesforce" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "LiveText for Salesforce (QA)" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "MyU App" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SMS Magic Interact" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Chatter" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Files" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Mobile Dashboards" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Social Customer Service (SCS)" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Touch" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce for Outlook" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce1 for Android" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce1 for iOS" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SalesforceA" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SalesforceIQ" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Workbench" : {
      "Max" : 0,
      "Remaining" : 0
    }
  },
  "DailyDurableGenericStreamingApiEvents" : {
    "Max" : 10000,
    "Remaining" : 10000
  },
  "DailyDurableStreamingApiEvents" : {
    "Max" : 10000,
    "Remaining" : 10000
  },
  "DailyGenericStreamingApiEvents" : {
    "Max" : 10000,
    "Remaining" : 10000,
    "AgilePoint" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Ant Migration Tool" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Axsy Server Integration" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Chatter Desktop" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Chatter Mobile for BlackBerry" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Dataloader Bulk" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Dataloader Partner" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "EAHelperBot" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Force.com IDE" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "LiveText for Salesforce" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "LiveText for Salesforce (QA)" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "MyU App" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SMS Magic Interact" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Chatter" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Files" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Mobile Dashboards" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Social Customer Service (SCS)" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Touch" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce for Outlook" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce1 for Android" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce1 for iOS" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SalesforceA" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SalesforceIQ" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Workbench" : {
      "Max" : 0,
      "Remaining" : 0
    }
  },
  "DailyStreamingApiEvents" : {
    "Max" : 20000,
    "Remaining" : 20000,
    "AgilePoint" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Ant Migration Tool" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Axsy Server Integration" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Chatter Desktop" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Chatter Mobile for BlackBerry" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Dataloader Bulk" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Dataloader Partner" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "EAHelperBot" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Force.com IDE" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "LiveText for Salesforce" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "LiveText for Salesforce (QA)" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "MyU App" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SMS Magic Interact" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Chatter" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Files" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Mobile Dashboards" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Social Customer Service (SCS)" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce Touch" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce for Outlook" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce1 for Android" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Salesforce1 for iOS" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SalesforceA" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "SalesforceIQ" : {
      "Max" : 0,
      "Remaining" : 0
    },
    "Workbench" : {
      "Max" : 0,
      "Remaining" : 0
    }
  },
  "DailyWorkflowEmails" : {
    "Max" : 20000,
    "Remaining" : 20000
  },
  "DataStorageMB" : {
    "Max" : 209,
    "Remaining" : 207
  },
  "DurableStreamingApiConcurrentClients" : {
    "Max" : 20,
    "Remaining" : 20
  },
  "FileStorageMB" : {
    "Max" : 209,
    "Remaining" : 206
  },
  "HourlyAsyncReportRuns" : {
    "Max" : 1200,
    "Remaining" : 1200
  },
  "HourlyDashboardRefreshes" : {
    "Max" : 200,
    "Remaining" : 200
  },
  "HourlyDashboardResults" : {
    "Max" : 5000,
    "Remaining" : 5000
  },
  "HourlyDashboardStatuses" : {
    "Max" : 999999999,
    "Remaining" : 999999999
  },
  "HourlyODataCallout" : {
    "Max" : 20000,
    "Remaining" : 19998
  },
  "HourlySyncReportRuns" : {
    "Max" : 500,
    "Remaining" : 500
  },
  "HourlyTimeBasedWorkflow" : {
    "Max" : 50,
    "Remaining" : 50
  },
  "MassEmail" : {
    "Max" : 5000,
    "Remaining" : 5000
  },
  "SingleEmail" : {
    "Max" : 5000,
    "Remaining" : 5000
  },
  "StreamingApiConcurrentClients" : {
    "Max" : 20,
    "Remaining" : 20
  }
}`
