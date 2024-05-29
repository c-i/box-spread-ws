package main

import (
	"context"
	"fmt"
	"log"

	"nhooyr.io/websocket"
)

type ConnData struct {
	Ctx    context.Context
	Conn   *websocket.Conn
	Cancel context.CancelFunc
}

type WssData struct {
	Op   string   `json:"op"`
	Data []string `json:"data"`
}

func dialWss(url string) (context.Context, *websocket.Conn, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	c, res, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		log.Fatalf("Dial error: %v", err)
	}
	fmt.Printf("%v\n\n", res)

	return ctx, c, cancel
}

func wssRead(ctx context.Context, c *websocket.Conn) ([]byte, error) {
	_, raw, err := c.Read(ctx)

	return raw, err
}

func connInit(exchanges Exchanges) map[string]ConnData {
	//initialises ws connections and starts reqLoops for selected exchanges

	connections := make(map[string]ConnData)
	if exchanges.Aevo {
		aevoCtx, aevoConn, aevoCancel := dialWss(AevoWss)
		connections["aevo"] = ConnData{aevoCtx, aevoConn, aevoCancel}

		go aevoWssReqLoop(aevoCtx, aevoConn)
	}

	if exchanges.Lyra {
		lyraCtx, lyraConn, lyraCancel := dialWss(LyraWss)
		connections["lyra"] = ConnData{lyraCtx, lyraConn, lyraCancel}

		go lyraWssReqLoop(lyraCtx, lyraConn)
	}

	return connections
}
