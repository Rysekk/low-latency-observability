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
	"github.com/shopspring/decimal"
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

type Trade struct {
	EventType string
	EventTime time.Time
	Symbol    string
	Price     decimal.Decimal
	Quantity  decimal.Decimal
	TradeTime time.Time
}

type message struct {
	payload []byte
	readAt  time.Time
}

func readStream(ctx context.Context, c chan message, conn *websocket.Conn, messageDropped prometheus.Counter, messageReceive prometheus.Counter) {
	defer close(c)
	for {
		_, result, err := conn.Read(ctx)
		queueStart := time.Now()
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
		case c <- message{payload: result, readAt: queueStart}:
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

func convertTrade(aggTrade AggTrade) (Trade, error) {
	var trade Trade
	trade.EventType = aggTrade.EventType
	trade.EventTime = time.UnixMilli(aggTrade.EventTime)
	trade.Symbol = aggTrade.Symbol
	price, err := decimal.NewFromString(aggTrade.Price)
	if err != nil {
		return Trade{}, err
	}
	trade.Price = price

	quantity, err := decimal.NewFromString(aggTrade.Quantity)
	if err != nil {
		return Trade{}, err
	}
	trade.Quantity = quantity
	trade.TradeTime = time.UnixMilli(aggTrade.TradeTime)
	return trade, nil
}

func main() {
	metricDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ingest_duration_seconds",
			Help:    "Duration of a stage in the pipeline.",
			Buckets: []float64{0.00001, 0.000025, 0.00005, 0.0001, 0.00025, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1},
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
	parseMalformedMessage := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ingest_message_malformed_total",
			Help: "The total number of messages that cannot be decoded as JSON.",
		})
	parseInvalidTrade := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "ingest_trade_invalid_total",
			Help: "The total number of decoded messages whose fields cannot be converted into a trade.",
		})

	channel := make(chan message, 128)
	prometheus.MustRegister(metricDuration, messageDropped, messageReceive, parseMalformedMessage, parseInvalidTrade)

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
		metricDuration.WithLabelValues("queue").Observe(time.Since(msg.readAt).Seconds())
		parseStart := time.Now()
		aggTrade, errJson := parseAggTrade(msg.payload)
		if errJson != nil {
			log.Printf("Json Parsing Error %v", errJson)
			parseMalformedMessage.Inc()
			continue
		}
		metricDuration.WithLabelValues("parse").Observe(time.Since(parseStart).Seconds())
		processingStart := time.Now()
		_, errTrade := convertTrade(aggTrade)
		if errTrade != nil {
			log.Printf("Trade Parsing Error %v", errTrade)
			parseInvalidTrade.Inc()
			continue
		}
		metricDuration.WithLabelValues("processing").Observe(time.Since(processingStart).Seconds())
		metricDuration.WithLabelValues("pipeline").Observe(time.Since(msg.readAt).Seconds())
	}
}
