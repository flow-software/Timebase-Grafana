package main

import (
	"os"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/plugin"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

func main() {
	// Blocks until Grafana stops the plugin. One Datasource instance is built
	// per configured datasource and rebuilt when its settings change.
	if err := datasource.Manage("flowsoftware-timebaseatlas-datasource", plugin.NewDatasource, datasource.ManageOpts{}); err != nil {
		log.DefaultLogger.Error(err.Error())
		os.Exit(1)
	}
}
