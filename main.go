package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"

	"github.com/judwhite/go-svc/svc"
)

const (
	namespace = "nsq"
)

type program struct {
	LogFile *os.File
	svr     *server
}

func (p program) Init(env svc.Environment) error {
	return nil
}

func (p program) Start() error {
	log.Info("Starting...\n")
	go p.svr.start()
	return nil
}

func (p program) Stop() error {
	log.Info("Stopping...\n")
	if err := p.svr.stop(); err != nil {
		return err
	}
	log.Info("Stopped.\n")
	return nil
}

var (
	listenAddress = flag.String("web.listen-address", ":9188", "Address to listen on for web interface and telemetry.")
	metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
	nsqdScrapeURI = flag.String("nsqd.scrape-uri", "http://localhost:4151/", "Base URI on which to scrape nsqd.")
	nsqTimeout    = flag.Duration("nsq.timeout", 5*time.Second, "Timeout for trying to get stats from NSQ.")
)

func main() {
	flag.Parse()

	prg := program{
		svr: &server{},
	}
	defer func() {
		if prg.LogFile != nil {
			prg.LogFile.Close()
		}
	}()

	// call svc.Run to start your program/service
	// svc.Run will call Init, Start, and Stop
	if err := svc.Run(prg); err != nil {
		log.Fatal(err)
	}
}

type server struct {
}

func (*server) start() {
	exporter := NewExporter(*nsqdScrapeURI, *nsqTimeout)
	prometheus.MustRegister(exporter)

	// Setup HTTP server
	http.Handle(*metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`<html>
             <head><title>NSQ Exporter</title></head>
             <body>
             <h1>NSQ Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
		if err != nil {
			log.Error(err)
		}
	})

	log.Infof("Starting Server: %s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}

func (*server) stop() error {
	return nil
}

// Exporter collects NSQ stats from the given URI and exports them using
// the prometheus metrics package.
type Exporter struct {
	NSQDStatsURI string
	mutex        sync.RWMutex
	client       *http.Client

	totalScrapes, jsonParseFailures prometheus.Counter
	up                              prometheus.Gauge

	startTime prometheus.Gauge
}

// NewExporter returns an initialized Exporter.
func NewExporter(baseURI string, timeout time.Duration) *Exporter {
	return &Exporter{
		NSQDStatsURI: strings.TrimRight(baseURI, "/") + "/stats?format=json",

		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the last scrape of nsqd successful.",
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_total_scrapes",
			Help:      "Current total nsqd scrapes.",
		}),
		jsonParseFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_json_parse_failures",
			Help:      "Number of errors while parsing JSON.",
		}),

		startTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "server_start",
			Help:      "Timestamp of nsqd startup.",
		}),

		client: &http.Client{
			Transport: &http.Transport{
				Dial: func(netw, addr string) (net.Conn, error) {
					c, err := net.DialTimeout(netw, addr, timeout)
					if err != nil {
						return nil, err
					}
					if err := c.SetDeadline(time.Now().Add(timeout)); err != nil {
						return nil, err
					}
					return c, nil
				},
			},
		},
	}
}

// Describe describes all the metrics ever exported by the NSQ exporter.
// It implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
	ch <- e.jsonParseFailures.Desc()
	ch <- e.startTime.Desc()
}

// Collect fetches the stats from nsqd and delivers them
// as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.scrape()

	ch <- e.up
	ch <- e.totalScrapes
	ch <- e.jsonParseFailures
	ch <- e.startTime
}

func (e *Exporter) scrape() {
	e.totalScrapes.Inc()

	stats, err := e.fetch(e.NSQDStatsURI)
	if err != nil {
		e.up.Set(0)
		log.Errorf("Can't scrape nsqd stats: %v", err)
		return
	}

	// TODO nsqlookupd

	e.up.Set(1)

	e.startTime.Set(float64(stats.StartTime))
	// TODO stats.Health
	// TODO stats.Version

	for _, topic := range stats.Topics {
		// TODO topic.TopicName
		// TODO topic.Depth
		// TODO topic.BackendDepth
		// TODO topic.MessageCount
		// TODO topic.Paused
		// topic.E2eProcessingLatency.Addr // missing?
		// topic.E2eProcessingLatency.Channel // missing?
		// topic.E2eProcessingLatency.Topic // missing?
		// TODO topic.E2eProcessingLatency.Count
		for key, value := range topic.E2eProcessingLatency.Percentiles {
			_, _ = key, value // TODO
		}
		// TODO
		for _, channel := range topic.Channels {
			// TODO channel.ChannelName
			// TODO channel.Depth
			// TODO channel.BackendDepth
			// TODO channel.InFlightCount
			// TODO channel.DeferredCount
			// TODO channel.MessageCount
			// TODO channel.RequeueCount
			// TODO channel.TimeoutCount
			// TODO channel.Paused
			for key, value := range channel.E2eProcessingLatency.Percentiles {
				_, _ = key, value // TODO
			}
			for _, client := range channel.Clients {
				_ = client
				// TODO client.Name // NOTE deprecated
				// TODO client.ClientID
				// TODO client.Hostname
				// TODO client.Version
				// TODO client.RemoteAddress
				// TODO client.State // TODO: not in ClientStats struct
				// TODO client.ReadyCount
				// TODO client.InFlightCount
				// TODO client.MessageCount
				// TODO client.FinishCount
				// TODO client.RequeueCount
				// TODO client.ConnectTs
				// TODO client.SampleRate
				// TODO client.Deflate
				// TODO client.Snappy
				// TODO client.UserAgent
				// TODO client.TLS
				// TODO client.CipherSuite // TODO: not in ClientStats struct
				// TODO client.TLSVersion
				// TODO client.TLSNegotiatedProtocol
				// TODO client.TLSNegotiatedProtocolIsMutual
			}
		}
	}
}

func (e *Exporter) fetch(uri string) (stats, error) {
	var nsqdStats stats
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nsqdStats, err
	}
	req.Header.Add("Accept", "application/vnd.nsq; version=1.0")
	resp, err := e.client.Do(req)
	if err != nil {
		return nsqdStats, err
	}
	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			log.Error(closeErr)
		}
	}()

	if resp.StatusCode != 200 {
		return nsqdStats, fmt.Errorf("status %d", resp.StatusCode)
	}

	err = json.NewDecoder(resp.Body).Decode(&nsqdStats)
	if err != nil {
		e.jsonParseFailures.Inc()
		return nsqdStats, fmt.Errorf("Can't read JSON: %v", err)
	}

	return nsqdStats, nil
}

type stats struct {
	Version   string       `json:"version"`
	Health    string       `json:"health"`
	StartTime int64        `json:"start_time"`
	Topics    []topicStats `json:"topics"`
}

type topicStats struct {
	Node         string          `json:"node"`
	Hostname     string          `json:"hostname"`
	TopicName    string          `json:"topic_name"`
	Depth        int64           `json:"depth"`
	MemoryDepth  int64           `json:"memory_depth"`
	BackendDepth int64           `json:"backend_depth"`
	MessageCount int64           `json:"message_count"`
	NodeStats    []*topicStats   `json:"nodes"`
	Channels     []*channelStats `json:"channels"`
	Paused       bool            `json:"paused"`

	E2eProcessingLatency *e2eProcessingLatencyAggregate `json:"e2e_processing_latency"`
}

type channelStats struct {
	Node          string          `json:"node"`
	Hostname      string          `json:"hostname"`
	TopicName     string          `json:"topic_name"`
	ChannelName   string          `json:"channel_name"`
	Depth         int64           `json:"depth"`
	MemoryDepth   int64           `json:"memory_depth"`
	BackendDepth  int64           `json:"backend_depth"`
	InFlightCount int64           `json:"in_flight_count"`
	DeferredCount int64           `json:"deferred_count"`
	RequeueCount  int64           `json:"requeue_count"`
	TimeoutCount  int64           `json:"timeout_count"`
	MessageCount  int64           `json:"message_count"`
	ClientCount   int             `json:"-"`
	Selected      bool            `json:"-"`
	NodeStats     []*channelStats `json:"nodes"`
	Clients       []*clientStats  `json:"clients"`
	Paused        bool            `json:"paused"`

	E2eProcessingLatency *e2eProcessingLatencyAggregate `json:"e2e_processing_latency"`
}

type e2eProcessingLatencyAggregate struct {
	Count       int                  `json:"count"`
	Percentiles []map[string]float64 `json:"percentiles"`
	Topic       string               `json:"topic"`
	Channel     string               `json:"channel"`
	Addr        string               `json:"host"`
}

type clientStats struct {
	Node              string        `json:"node"`
	RemoteAddress     string        `json:"remote_address"`
	Name              string        `json:"name"` // TODO: deprecated, remove in 1.0
	Version           string        `json:"version"`
	ClientID          string        `json:"client_id"`
	Hostname          string        `json:"hostname"`
	UserAgent         string        `json:"user_agent"`
	ConnectTs         int64         `json:"connect_ts"`
	ConnectedDuration time.Duration `json:"connected"`
	InFlightCount     int           `json:"in_flight_count"`
	ReadyCount        int           `json:"ready_count"`
	FinishCount       int64         `json:"finish_count"`
	RequeueCount      int64         `json:"requeue_count"`
	MessageCount      int64         `json:"message_count"`
	SampleRate        int32         `json:"sample_rate"`
	Deflate           bool          `json:"deflate"`
	Snappy            bool          `json:"snappy"`
	Authed            bool          `json:"authed"`
	AuthIdentity      string        `json:"auth_identity"`
	AuthIdentityURL   string        `json:"auth_identity_url"`

	TLS                           bool   `json:"tls"`
	CipherSuite                   string `json:"tls_cipher_suite"`
	TLSVersion                    string `json:"tls_version"`
	TLSNegotiatedProtocol         string `json:"tls_negotiated_protocol"`
	TLSNegotiatedProtocolIsMutual bool   `json:"tls_negotiated_protocol_is_mutual"`
}
