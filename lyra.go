package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

const LyraHttp string = "https://api.lyra.finance"
const LyraWss string = "wss://api.lyra.finance/ws"

func lyraMarkets(asset string) map[string]interface{} {
	url := LyraHttp + "/public/get_instruments"

	payload := strings.NewReader(fmt.Sprintf("{\"expired\":false,\"instrument_type\":\"option\",\"currency\":\"%v\"}", asset))

	req, _ := http.NewRequest("POST", url, payload)

	req.Header.Add("accept", "application/json")
	req.Header.Add("content-type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("lyraMarkets: request error: %v", err)
	}

	defer res.Body.Close()

	var markets map[string]interface{}

	decoder := json.NewDecoder(res.Body)
	err = decoder.Decode(&markets)
	if err != nil {
		log.Fatalf("markets json decode error: %v", err)
	}

	return markets
}

func lyraInstruments(markets map[string]interface{}) []string {
	var instruments []string
	result, ok := markets["result"].([]interface{})
	if !ok {
		log.Printf("lyraInstruments: unable to convert markets['result'] to []interface{}")
		return instruments
	}

	var instrument string
	var market map[string]interface{}
	for _, item := range result {
		market = item.(map[string]interface{})
		instrument = market["instrument_name"].(string)

		instruments = append(instruments, instrument)
	}

	return instruments
}

func lyraOrderbookJson(instruments []string) []byte {
	params := make(map[string][]string)
	params["channels"] = []string{}

	var param string
	for _, instrument := range instruments {
		param = "orderbook." + instrument + ".10.10"
		params["channels"] = append(params["channels"], param)
	}

	data := struct {
		Id     string              `json:"id"`
		Method string              `json:"method"`
		Params map[string][]string `json:"params"`
	}{
		"2",
		"subscribe",
		params,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Fatalf("orderbook json marshal error: %v", err)
	}

	return jsonData
}

func lyraWssReqOrderbook(instruments []string, ctx context.Context, c *websocket.Conn) {
	var data []byte
	for i := 0; true; i += 20 {
		if i+20 < len(instruments) {
			data = lyraOrderbookJson(instruments[i : i+20])
		} else {
			data = lyraOrderbookJson(instruments[i:])
		}

		// fmt.Printf("subscribe: %v\n\n", string(data))
		err := c.Write(ctx, 1, data)
		if err != nil {
			log.Fatalf("Write error: %v\n", err)
		}

		if i+20 > len(instruments) {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func lyraUpdateOrderbooks(data map[string]interface{}) error {
	lyraInstrument, ok := data["instrument_name"].(string)
	bidsRaw, bidsOk := data["bids"].([]interface{})
	asksRaw, asksOk := data["asks"].([]interface{})
	if !ok || !(bidsOk || asksOk) {
		return fmt.Errorf("lyraUpdateOrderbooks: unable to convert field: response: %+v", data)
	}

	if len(bidsRaw) <= 0 && len(asksRaw) <= 0 {
		return fmt.Errorf("bidsRaw and asksRaw empty response")
	}

	components := strings.Split(lyraInstrument, "-")
	strike, err := strconv.ParseFloat(components[2], 64)
	if err != nil {
		return fmt.Errorf("lyraUpdateOrderbooks: time.Parse error: %v", err)
	}
	expiryTs, err := time.Parse("20060102", components[1])
	if err != nil {
		return fmt.Errorf("lyraUpdateOrderbooks: time.Parse error: %v", err)
	}
	expiry := expiryTs.Unix()
	optionType := components[3]

	bids, bidsErr := unpackOrders(bidsRaw, strike, optionType, "lyra")
	asks, asksErr := unpackOrders(asksRaw, strike, optionType, "lyra")
	if bidsErr != nil && asksErr != nil {
		return fmt.Errorf("unpackOrders error: \n%v\n%v", bidsErr, asksErr)
	}

	updateOrderbook(expiry, bids, asks)

	return nil
}

func lyraWssRead(ctx context.Context, c *websocket.Conn) {
	var res map[string]interface{}
	raw, err := wssRead(ctx, c)
	if err != nil {
		log.Printf("lyraWssRead: %v\n(response): %v\n\n", err, string(raw))
		return
	}

	err = json.Unmarshal(raw, &res)
	if err != nil {
		log.Printf("lyraWssRead: error unmarshaling orderbookRaw: %v\n(response): %v\n\n", err, string(raw))
		return
	}

	params, ok := res["params"].(map[string]interface{})
	if !ok {
		log.Printf("lyraWssRead: unable to convert res['params'] to map[string]interface{}: (raw response): %v\n\n", string(raw))
		return
	}

	data, ok := params["data"].(map[string]interface{})
	channel, chanOk := params["channel"].(string)
	if !ok || !chanOk {
		log.Printf("lyraWssRead: unable to convert params['data'] to map[string]interface{} or params['channel'] to string: (raw response): %v\n dataOk: %v\nchannelOk: %v\n\n", string(raw), ok, chanOk)
		return
	}
	// fmt.Printf("%+v\n\n", res)

	if strings.Contains(channel, "orderbook") {
		lyraUpdateOrderbooks(data)
	}
}

func lyraWssReqLoop(ctx context.Context, c *websocket.Conn) {
	for {
		markets := lyraMarkets("ETH")
		instruments := lyraInstruments(markets)
		fmt.Printf("Lyra number of instruments: %v\n\n", len(instruments))

		lyraWssReqOrderbook(instruments, ctx, c)
		log.Printf("Requested Lyra Orderbooks")

		time.Sleep(time.Minute * 10)
	}
}
