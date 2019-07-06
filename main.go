package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	namespace               string = "gbfs"
	minimumPollSleepSeconds int64  = 10
	// Port list: https://github.com/prometheus/prometheus/wiki/Default-port-allocations
	listenAddress string = ":9607"
)

var (
	bikesAvailable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "bikes_available",
	}, []string{"station_id"})
	bikesDisabled = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "bikes_disabled",
	}, []string{"station_id"})
	docksAvailable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "docks_available",
	}, []string{"station_id"})
)

// Max returns the greatest of the two integers.
func Max(x, y int64) int64 {
	if x > y {
		return x
	}
	return y
}

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
	resp, err := http.Get("https://tor.publicbikesystem.net/ube/gbfs/v1/en/station_status")
	if err != nil {
		log.Fatalln(err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalln(err)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(bikesAvailable)
	registry.MustRegister(bikesDisabled)
	registry.MustRegister(docksAvailable)

	stationStatusResp, _ := GetStationStatuses(body)
	for _, status := range stationStatusResp.Data.Stations {
		bikesAvailable.With(prometheus.Labels{"station_id": status.ID}).Set(float64(status.BikesAvailable))
		bikesDisabled.With(prometheus.Labels{"station_id": status.ID}).Set(float64(status.BikesDisabled))
		docksAvailable.With(prometheus.Labels{"station_id": status.ID}).Set(float64(status.DocksAvailable))
	}

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	handler.ServeHTTP(w, r)
}

func main() {
	log.Printf("G O O D B O I  L A U N C H I N G  ON  %s\n", listenAddress)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/probe", probeGBFS)
	http.ListenAndServe(listenAddress, nil)
}
