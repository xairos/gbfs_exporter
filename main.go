package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	namespace string = "gbfs"
	// Port list: https://github.com/prometheus/prometheus/wiki/Default-port-allocations
	listenAddress string = ":9607"
)

/***********
 BEGIN UTIL
***********/

// Max returns the greatest of the two integers.
func Max(x, y int64) int64 {
	if x > y {
		return x
	}
	return y
}

// BoolToFloat64 converts a boolean value to a float. 1 if true, else 0.
func BoolToFloat64(x bool) float64 {
	if x {
		return 1
	}
	return 0
}

/***********
 END UTIL
***********/

// GBFSAPIResponse yada yada
type GBFSAPIResponse struct {
	LastUpdated int64 `json:"last_updated"`
	TTL         int64 `json:"ttl"`
}

// StationStatus holds the status of stations
type StationStatus struct {
	ID             string `json:"station_id"`
	BikesAvailable int64  `json:"num_bikes_available"`
	BikesDisabled  int64  `json:"num_bikes_disabled,omitempty"`
	DocksAvailable int64  `json:"num_docks_available"`
	DocksDisabled  int64  `json:"num_docks_disabled,omitempty"`
	Installed      bool   `json:"is_installed"`
	Renting        bool   `json:"is_renting"`
	Returning      bool   `json:"is_returning"`
	LastReported   int64  `json:"last_reported"`
}

// UnmarshalJSON I hate warnings
func (s *StationStatus) UnmarshalJSON(data []byte) error {
	type Alias StationStatus
	alias := &struct {
		Installed int64 `json:"is_installed"`
		Renting   int64 `json:"is_renting"`
		Returning int64 `json:"is_returning"`

		*Alias
	}{
		Alias: (*Alias)(s),
	}

	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	s.Installed = alias.Installed != 0
	s.Renting = alias.Renting != 0
	s.Returning = alias.Returning != 0
	return nil
}

// StationStatusAPIResponse holds the API response for station status
type StationStatusAPIResponse struct {
	Data struct {
		Stations []StationStatus `json:"stations"`
	} `json:"data"`
	GBFSAPIResponse
}

// GetStationStatuses blah blah
func GetStationStatuses(body []byte) (*StationStatusAPIResponse, error) {
	resp := new(StationStatusAPIResponse)
	err := json.Unmarshal(body, &resp)
	return resp, err
}

func probeGBFS(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	target := params.Get("target")
	if target == "" {
		http.Error(w, "Target parameter missing", 400)
		return
	}

	resp, err := http.Get(target)
	if err != nil {
		// Room for improvement, check types of errors that can be returned (ex. timeouts, redirects)
		http.Error(w, fmt.Sprintf("HTTP error: %v", err), 400)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read HTTP body of target '%s': %v", target, err), 500)
		return
	}

	var (
		bikesAvailableGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "bikes_available",
			Help:      "The number of bikes available for rental",
		}, []string{"station_id"})
		bikesDisabledGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "bikes_disabled",
			Help:      "The number of disabled bikes",
		}, []string{"station_id"})
		docksAvailableGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "docks_available",
			Help:      "The number of docks accepting bike returns",
		}, []string{"station_id"})
		docksDisabledGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "docks_disabled",
			Help:      "The number of empty but disabled dock points",
		}, []string{"station_id"})
		installedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "installed",
			Help: "Indicates if the station is currently renting bikes, " +
				"regardless of if any bikes are available",
		}, []string{"station_id"})
		rentingGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "renting",
			Help: "Indicates if the station is currently accepting bike returns, " +
				"regardless of if any docks are available",
		}, []string{"station_id"})
		lastReportedGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_reported_timestamp_seconds",
			Help:      "Last time this station reported its status to the feed, in unixtime",
		}, []string{"station_id"})
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(bikesAvailableGauge)
	registry.MustRegister(bikesDisabledGauge)
	registry.MustRegister(docksAvailableGauge)
	registry.MustRegister(docksDisabledGauge)
	registry.MustRegister(installedGauge)
	registry.MustRegister(rentingGauge)
	registry.MustRegister(lastReportedGauge)

	stationStatusResp, err := GetStationStatuses(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Could not unmarshal target JSON,"+
			" target '%s' does not have the expected schema: %v", target, err), 400)
		return
	}

	for _, status := range stationStatusResp.Data.Stations {
		bikesAvailableGauge.With(prometheus.Labels{"station_id": status.ID}).Set(float64(status.BikesAvailable))
		bikesDisabledGauge.With(prometheus.Labels{"station_id": status.ID}).Set(float64(status.BikesDisabled))
		docksAvailableGauge.With(prometheus.Labels{"station_id": status.ID}).Set(float64(status.DocksAvailable))
		docksDisabledGauge.With(prometheus.Labels{"station_id": status.ID}).Set(float64(status.DocksDisabled))
		installedGauge.With(prometheus.Labels{"station_id": status.ID}).Set(BoolToFloat64(status.Installed))
		rentingGauge.With(prometheus.Labels{"station_id": status.ID}).Set(BoolToFloat64(status.Renting))
		lastReportedGauge.With(prometheus.Labels{"station_id": status.ID}).Set(float64(status.LastReported))
	}

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	handler.ServeHTTP(w, r)
}

func main() {
	log.Printf("G O O D B O I  L A U N C H I N G  ON  %s\n", listenAddress)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/probe", probeGBFS)
	if err := http.ListenAndServe(listenAddress, nil); err != nil {
		log.Fatalln(errors.Wrapf(err, "Failed to spin up server"))
	}
}
