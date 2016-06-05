package mandrill

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/influxdata/telegraf"
)

type MandrillWebhook struct {
	Path string
	acc  telegraf.Accumulator
}

func (md *MandrillWebhook) Register(router *mux.Router, acc telegraf.Accumulator) {
	router.HandleFunc(md.Path, md.eventHandler).Methods("POST")
	log.Printf("Started the webhooks_mandrill on %s\n", md.Path)
	md.acc = acc
}

func (md *MandrillWebhook) eventHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var events []MandrillEvent
	err = json.Unmarshal(data, &events)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	for _, event := range events {
		md.acc.AddFields("mandrill_webhooks", event.Fields(), event.Tags(), time.Unix(event.TimeStamp, 0))
	}

	w.WriteHeader(http.StatusOK)
}
