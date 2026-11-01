package main

import (
	"context"
	"encoding/json"
	"log/slog"
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

func (t AggTrade) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("EventType", t.EventType),
		slog.Int64("EventTime", t.EventTime),
		slog.String("Symbol", t.Symbol),
		slog.String("Price", t.Price),
		slog.String("Quantity", t.Quantity),
		slog.Int64("TradeTime", t.TradeTime),
	)
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

type logThrottle struct {
	interval   time.Duration
	last       time.Time
	suppressed int
}

func (t *logThrottle) allow(now time.Time) (ok bool, suppressed int) {
	if now.Sub(t.last) >= t.interval {
		t.last = now

		n := t.suppressed
		t.suppressed = 0

		return true, n
	}
	t.suppressed = t.suppressed + 1
	return false, 0
}

func readStream(ctx context.Context, c chan message, conn *websocket.Conn, messageDropped prometheus.Counter,
	messageReceive prometheus.Counter, logger *slog.Logger) {
	defer close(c)
	for {
		_, result, err := conn.Read(ctx)
		queueStart := time.Now()
		if err != nil {
			if ctx.Err() != nil {
				logger.Info("shutdown requested")
				return
			} else {
				logger.Error("connection error",
					slog.String("error", err.Error()),
				)
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

func httpPrometheusExporter(logger *slog.Logger) {
	http.Handle("/metrics", promhttp.Handler())
	logger.Error("metrics server failed",
		slog.Any("error", http.ListenAndServe(":8080", nil)),
	)
	os.Exit(1)
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	malformedThrottle := logThrottle{interval: 10 * time.Second}
	invalidThrottle := logThrottle{interval: 10 * time.Second}
	prometheus.MustRegister(metricDuration, messageDropped, messageReceive, parseMalformedMessage, parseInvalidTrade)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	conn, resp, err := websocket.Dial(ctx, wsendpoint, nil)
	if err != nil {
		status := "none"
		if resp != nil {
			status = resp.Status
		}
		logger.Error("fail to connect to the websocket",
			slog.String("error", err.Error()),
			slog.String("websocket_url", wsendpoint),
			slog.String("http_response", status),
		)
		os.Exit(1)
	} else {
		logger.Info("connected to the websocket",
			slog.String("websocket_url", wsendpoint),
			slog.String("http_response", resp.Status),
		)
	}
	defer conn.Close(websocket.StatusNormalClosure, "Client closing connection")

	go readStream(ctx, channel, conn, messageDropped, messageReceive, logger)
	go httpPrometheusExporter(logger)
	for msg := range channel {
		metricDuration.WithLabelValues("queue").Observe(time.Since(msg.readAt).Seconds())
		parseStart := time.Now()
		aggTrade, errJson := parseAggTrade(msg.payload)
		if errJson != nil {
			parseMalformedMessage.Inc()
			if ok, suppressed := malformedThrottle.allow(time.Now()); ok {
				logger.Error("malformed message",
					slog.String("error", errJson.Error()),
					slog.String("payload", string(msg.payload[:min(len(msg.payload), 1024)])),
					slog.Int("suppressed", suppressed),
				)
			}
			continue
		}
		metricDuration.WithLabelValues("parse").Observe(time.Since(parseStart).Seconds())
		processingStart := time.Now()
		_, errTrade := convertTrade(aggTrade)
		if errTrade != nil {
			parseInvalidTrade.Inc()
			if ok, suppressed := invalidThrottle.allow(time.Now()); ok {
				logger.Error("invalid trade",
					slog.String("error", errTrade.Error()),
					slog.Any("trade", aggTrade),
					slog.Int("suppressed", suppressed),
				)
			}
			continue
		}
		metricDuration.WithLabelValues("processing").Observe(time.Since(processingStart).Seconds())
		metricDuration.WithLabelValues("pipeline").Observe(time.Since(msg.readAt).Seconds())
	}
}
