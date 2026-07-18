package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/coder/websocket"
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

func readStream(ctx context.Context, c chan []byte, conn *websocket.Conn) {
	for {
		_, result, err := conn.Read(ctx)
		if err != nil {
			log.Fatalf("Read Error %v", err)
			return
		}
		select {
		case c <- result:
		default:
			log.Println("Message drop")
		}

	}
}

func main() {
	var aggTrade AggTrade
	channel := make(chan []byte, 128)
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsendpoint, nil)
	if err != nil {
		log.Fatalf("Fail to connect to the websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "Client closing connection")

	go readStream(ctx, channel, conn)
	for msg := range channel {
		err := json.Unmarshal(msg, &aggTrade)
		if err != nil {
			log.Printf("Json Parsing Error %v", err)
		}
		log.Println(aggTrade)
	}
}
