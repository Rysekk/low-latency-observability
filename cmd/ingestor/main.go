package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

func readStream(ctx context.Context, c chan []byte, conn *websocket.Conn, messageDropped prometheus.Counter, messageReceive prometheus.Counter) {
	defer close(c)
	for {
		_, result, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Println("shutdown requested")
				return
			} else {
				log.Printf("Read Error %v", err)
				return
			}
		}
		messageReceive.Inc()
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

func parseAggTrade(msg []byte) (AggTrade, error) {
	var aggTrade AggTrade
	err := json.Unmarshal(msg, &aggTrade)
	if err != nil {
		return AggTrade{}, err
	}
	return aggTrade, nil
}

func main() {
	metricDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ingest_duration_seconds",
			Help:    "Duration of a stage in the pipeline.",
			Buckets: prometheus.ExponentialBuckets(0.00001, 1.5, 15),
		},
		[]string{"stage"},
	)
	messageDropped := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ingest_message_dropped_total",
			Help: "The total number of message dropped.",
		})
	messageReceive := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ingest_message_receive_total",
			Help: "The total number of message ingested.",
		})
	parseError := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ingest_parse_errors_total",
			Help: "The total number of error in the parsing pipeline.",
		})
	channel := make(chan []byte, 128)
	prometheus.MustRegister(metricDuration, messageDropped, messageReceive, parseError)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	conn, _, err := websocket.Dial(ctx, wsendpoint, nil)
	if err != nil {
		log.Fatalf("Fail to connect to the websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "Client closing connection")

	go readStream(ctx, channel, conn, messageDropped, messageReceive)
	go httpPrometheusExporter()
	for msg := range channel {
		pipelineStart := time.Now()
		_, err := parseAggTrade(msg)
		if err != nil {
			log.Printf("Json Parsing Error %v", err)
			parseError.Inc()
			continue
		}
		metricDuration.WithLabelValues("parse").Observe(time.Since(pipelineStart).Seconds())
		processingStart := time.Now()
		metricDuration.WithLabelValues("processing").Observe(time.Since(processingStart).Seconds())
		metricDuration.WithLabelValues("pipeline").Observe(time.Since(pipelineStart).Seconds())
	}
}
