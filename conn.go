package main

import (
	"context"
	"fmt"
	"log"

	"nhooyr.io/websocket"
)

type connData struct {
	Ctx    context.Context
	Conn   *websocket.Conn
	Cancel context.CancelFunc
}

type wssData struct {
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

func connInit() {
	aevoCtx, aevoConn, aevoCancel := dialWss(AevoWss)
	lyraCtx, lyraConn, lyraCancel := dialWss(LyraWss)
	connections := map[string]connData{
		"aevo": {aevoCtx, aevoConn, aevoCancel},
		"lyra": {lyraCtx, lyraConn, lyraCancel},
	}
	defer aevoCancel()
	defer aevoConn.Close(websocket.StatusNormalClosure, "")
	defer aevoConn.CloseNow()
	defer lyraCancel()
	defer lyraConn.Close(websocket.StatusNormalClosure, "")
	defer lyraConn.CloseNow()

	go aevoWssReqLoop(aevoCtx, aevoConn)
	lyraWssReqLoop(lyraCtx, lyraConn)

}
