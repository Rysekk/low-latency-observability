package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const wsendpoint = "wss://stream.binance.com:9443/ws/btcusdt@aggTrade"

type AggTrade struct {
	EventType string `json:"e"`
	EventTime int64  `json:"E"`
	Symbol    string `json:"s"`
	Price     string `json:"p"`
	Quantity  string `json:"q"`
	TradeTime int64  `json:"T"`
}

func readStream(ctx context.Context, c chan []byte, conn *websocket.Conn, messageDropped prometheus.Counter) {
	for {
		_, result, err := conn.Read(ctx)
		if err != nil {
			log.Fatalf("Read Error %v", err)
			return
		}
		select {
		case c <- result:
		default:
			messageDropped.Inc()
		}

	}
}

func httpPrometheusExporter() {
	http.Handle("/metrics", promhttp.Handler())
	log.Fatalln(http.ListenAndServe(":8080", nil))
}

func main() {
	var aggTrade AggTrade
	channel := make(chan []byte, 128)
	metricDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ingest_duration_seconds",
			Help:    "Duration of a stage in the pipeline",
			Buckets: prometheus.ExponentialBuckets(0.00001, 1.5, 15),
		},
		[]string{"stage"},
	)
	messageDropped := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ingest_message_dropped_total",
			Help: "The total number of message dropped.",
		})
	prometheus.MustRegister(metricDuration, messageDropped)
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsendpoint, nil)
	if err != nil {
		log.Fatalf("Fail to connect to the websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "Client closing connection")

	go readStream(ctx, channel, conn, messageDropped)
	go httpPrometheusExporter()
	for msg := range channel {
		pipelineStart := time.Now()
		err := json.Unmarshal(msg, &aggTrade)
		if err != nil {
			log.Printf("Json Parsing Error %v", err)
		}
		metricDuration.WithLabelValues("parse").Observe(time.Since(pipelineStart).Seconds())
		processingStart := time.Now()
		log.Println(aggTrade)
		metricDuration.WithLabelValues("processing").Observe(time.Since(processingStart).Seconds())
		metricDuration.WithLabelValues("pipeline").Observe(time.Since(pipelineStart).Seconds())
	}
}
