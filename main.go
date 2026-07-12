package main

import (
	"context"
	"fmt"
	"log"

	"github.com/coder/websocket"
)

const wsendpoint = "wss://stream.binance.com:9443/ws/btcusdt@aggTrade"

func main() {
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsendpoint, nil)
	if err != nil {
		log.Fatalf("Fail to connect to the websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "Client closing connection")

	for {
		_, result, err := conn.Read(ctx)
		if err != nil {
			log.Fatalf("Read Error %v", err)
			return
		}
		fmt.Println(string(result))
	}

}
