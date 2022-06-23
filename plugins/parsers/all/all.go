package all

import (
	//Blank imports for plugins to register themselves
	_ "github.com/influxdata/telegraf/plugins/parsers/csv"
	_ "github.com/influxdata/telegraf/plugins/parsers/json"
	_ "github.com/influxdata/telegraf/plugins/parsers/json_v2"
	_ "github.com/influxdata/telegraf/plugins/parsers/wavefront"
	_ "github.com/influxdata/telegraf/plugins/parsers/xpath"
)
