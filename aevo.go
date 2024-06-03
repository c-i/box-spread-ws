package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

const AevoHttp string = "https://api.aevo.xyz"
const AevoWss string = "wss://ws.aevo.xyz"

func aevoMarkets(asset string) []interface{} {
	url := AevoHttp + "/markets?asset=" + asset + "&instrument_type=OPTION"

	req, _ := http.NewRequest("GET", url, nil) //NewRequest + Client.Do used to pass headers, otherwise http.Get can be used

	req.Header.Add("accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("aevoMarkets request error: %v", err)
	}

	defer res.Body.Close() //Client.Do, http.Get, http.Post, etc all need response Body to be closed when done reading from it
	// defer defers execution until enclosing function returns

	var markets []interface{}

	decoder := json.NewDecoder(res.Body)
	err = decoder.Decode(&markets)
	if err != nil {
		log.Fatalf("aevoMarkets json decode error: %v", err)
	}

	return markets
}

func aevoInstruments(markets []interface{}) []string {
	var instruments []string
	var market map[string]interface{}
	var isActive bool
	var instrumentName string
	for _, item := range markets {
		market = item.(map[string]interface{})
		isActive = market["is_active"].(bool)
		if isActive {
			instrumentName = market["instrument_name"].(string)
			instruments = append(instruments, instrumentName)
		}
	}

	return instruments
}

func aevoOrderbookJson(instruments []string) []byte {
	var orderbooks []string
	for _, instrument := range instruments {
		orderbooks = append(orderbooks, "orderbook:"+instrument)
	}

	data := WssData{
		Op:   "subscribe",
		Data: orderbooks,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Fatalf("orderbook json marshal error: %v", err)
	}

	return jsonData
}

func aevoWssReqOrderbook(instruments []string, ctx context.Context, c *websocket.Conn) {
	var data []byte
	for i := 0; true; i += 20 {
		if i+20 < len(instruments) {
			data = aevoOrderbookJson(instruments[i : i+20])
		} else {
			data = aevoOrderbookJson(instruments[i:])
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

func aevoUpdateExistingOrderbook(order *Orders, bids []Order, asks []Order) {
	exchange := bids[0].Exchange //if multiple exchanges are used and bids is empty must add check and use asks instead
	optionType := bids[0].OptionType
	_, exists := order.CallBids[exchange]
	if !exists {
		order.CallBids = make(map[string][]Order)
		order.CallAsks = make(map[string][]Order)
		order.PutBids = make(map[string][]Order)
		order.PutAsks = make(map[string][]Order)
	}

	if optionType == "C" {

		order.CallBids[exchange] = bids
		order.CallAsks[exchange] = asks

		//should only need to sort when modified
		// bids should be sorted descending, asks ascending
		// sorting should be unnecessary if exchange sends data correctly, but I'll sort for now anyway
		sort.Slice(order.CallBids[exchange], func(i, j int) bool {
			return order.CallBids[exchange][i].Price > order.CallBids[exchange][j].Price
		})
		sort.Slice(order.CallAsks[exchange], func(i, j int) bool {
			return order.CallAsks[exchange][i].Price < order.CallAsks[exchange][j].Price
		})
	}
	if optionType == "P" {
		order.PutBids[exchange] = bids
		order.PutAsks[exchange] = asks

		sort.Slice(order.PutBids[exchange], func(i, j int) bool {
			return order.PutBids[exchange][i].Price > order.PutBids[exchange][j].Price
		})
		sort.Slice(order.PutAsks[exchange], func(i, j int) bool {
			return order.PutAsks[exchange][i].Price < order.PutAsks[exchange][j].Price
		})
	}
}

func aevoUpdateOrderbook(expiry int64, bids []Order, asks []Order) {
	_, exists := Orderbooks[expiry]
	// remember to sort by strike
	if !exists { //might be unnecessary
		Orderbooks[expiry] = make([]*Orders, 0)
	}

	strike := bids[0].Strike

	strikeExists := false

	for _, order := range Orderbooks[expiry] {
		if order.Strike == strike {

			aevoUpdateExistingOrderbook(order, bids, asks)

			// fmt.Printf("%+v\n\n", order)

			strikeExists = true
			break
		}
	}

	if !strikeExists {
		exchange := bids[0].Exchange
		optionType := bids[0].OptionType
		strikeOrder := Orders{Strike: strike}
		if optionType == "C" {
			strikeOrder.CallBids = make(map[string][]Order)
			strikeOrder.CallAsks = make(map[string][]Order)

			strikeOrder.CallBids[exchange] = bids
			strikeOrder.CallAsks[exchange] = asks
		}
		if optionType == "P" {
			strikeOrder.PutBids = make(map[string][]Order)
			strikeOrder.PutAsks = make(map[string][]Order)

			strikeOrder.PutBids[exchange] = bids
			strikeOrder.PutAsks[exchange] = asks
		}

		Orderbooks[expiry] = append(Orderbooks[expiry], &strikeOrder)
		// should only need to sort []Orders by Strike when new item is added
		sort.Slice(Orderbooks[expiry], func(i, j int) bool {
			return Orderbooks[expiry][i].Strike < Orderbooks[expiry][j].Strike
		})
	}
}

func aevoUpdateOrderbooks(res map[string]interface{}) error {
	//takes unmarshaled ws response and updates Orderbooks

	data, ok := res["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("aevoUpdateOrderbooks: unable to cast response to type map[string]interface{}: response: %+v", res)
	}

	// if len(data) <= 3 { //check for ping response, not very robust and inappropriate to catch here, might need to fix later
	// 	return
	// }

	instrument, ok := data["instrument_name"].(string)
	if !ok {
		return fmt.Errorf("aevoUpdateOrderbooks: unable to cast data['instrument_name'] to type string: response: %+v", res)
	}
	components := strings.Split(instrument, "-")
	expiryTime, err1 := time.Parse("02Jan06", components[1])
	expiry := expiryTime.Unix()
	strike, err2 := strconv.ParseFloat(components[2], 64)
	optionType := components[3]
	if err1 != nil || err2 != nil {
		return fmt.Errorf("unpackOrders error: \n%v\n%v", err1, err2)
	}

	bidsRaw, bidsOk := data["bids"].([]interface{})
	asksRaw, asksOk := data["asks"].([]interface{})
	if !bidsOk || !asksOk {
		return fmt.Errorf("aevoUpdateOrderbooks: unable to convert field: response: %+v", res)
	}

	if len(bidsRaw) <= 0 || len(asksRaw) <= 0 { //if instrument has no bids/asks its discarded
		// should be && or when there are multiple exchanges used and || when only one, fix
		return errors.New("no bids and asks")
	}

	bids, bidsErr := unpackOrders(bidsRaw, strike, optionType, "aevo")
	asks, asksErr := unpackOrders(asksRaw, strike, optionType, "aevo")
	if bidsErr != nil && asksErr != nil {
		return fmt.Errorf("unpackOrders error: \n%v\n%v", bidsErr, asksErr)
	}

	aevoUpdateOrderbook(expiry, bids, asks)

	return nil
}

func aevoWssRead(ctx context.Context, c *websocket.Conn) { //add exit condition, add ping or use Reader instead of Read to automatically manage ping, disconnect, etc
	//reads for ws response and updates Orderbooks

	var res map[string]interface{}
	raw, err := wssRead(ctx, c)
	if err != nil {
		log.Printf("aevoWssRead: %v\n(response): %v\n\n", err, string(raw))
		return
	}

	err = json.Unmarshal(raw, &res)
	if err != nil {
		log.Printf("aevoWssRead: error unmarshaling orderbookRaw: %v\n\n", err)
		return
	}

	channel, ok := res["channel"].(string)
	if !ok {
		log.Printf("aevoWssRead: unable to convert response 'channel' to string\n\n")
		return
	}

	if strings.Contains(channel, "orderbook") {
		aevoUpdateOrderbooks(res)
		updateBoxes()
		fmt.Printf("%+v\n\n", len(Boxes))
		// fmt.Printf("%+v\n\n", Boxes)
		// fmt.Printf("%+v\n\n", res)
	}
}

func aevoWssReqLoop(ctx context.Context, c *websocket.Conn) {
	for {
		markets := aevoMarkets("ETH")
		instruments := aevoInstruments(markets)
		fmt.Printf("Aevo number of instruments: %v\n\n", len(instruments))

		aevoWssReqOrderbook(instruments, ctx, c)
		log.Printf("Requested Aevo Orderbooks")

		time.Sleep(time.Minute * 10)
	}
}
