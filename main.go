package main

import (
	"errors"
	"log"
	"strconv"

	"nhooyr.io/websocket"
)

type Order struct {
	Price    float64
	Amount   float64
	Iv       float64
	Exchange string
}

type Orders struct {
	CallBids []Order
	CallAsks []Order
	PutBids  []Order
	PutAsks  []Order
}

type Exchanges struct {
	Aevo bool
	Lyra bool
}

var Orderbooks = make(map[int64]map[float64]*Orders)
var Boxes = make(map[int64]*Box)

func unpackOrders(orders []interface{}, exchange string) ([]Order, error) {
	//takes unmarshaled json arrays of bids/asks and returns []Order
	//expects orders []interface{} to unpack into 2d array of [[price, amount, IV]...]

	unpackedOrders := make([]Order, 0)
	for _, order := range orders {
		orderArr, ok := order.([]interface{})

		if !ok {
			return unpackedOrders, errors.New("orders not of []interface{} type")
		}
		if exchange == "aevo" && len(orderArr) != 3 {
			return unpackedOrders, errors.New("aevo orders not length 3")
		}
		if exchange == "lyra" && len(orderArr) != 2 {
			return unpackedOrders, errors.New("lyra orders not length 2")
		}

		priceStr, priceOk := orderArr[0].(string)
		amountStr, amountOk := orderArr[1].(string)
		var ivStr string
		var ivOk bool
		if exchange == "aevo" {
			ivStr, ivOk = orderArr[2].(string)
		}
		if exchange == "lyra" {
			ivStr = "-1"
			ivOk = true
		}
		if !priceOk || !amountOk || !ivOk {
			return unpackedOrders, errors.New("unable to convert interface{} element to string")
		}

		price, priceErr := strconv.ParseFloat(priceStr, 64)
		amount, amountErr := strconv.ParseFloat(amountStr, 64)
		iv, ivErr := strconv.ParseFloat(ivStr, 64)
		if priceErr != nil || amountErr != nil || ivErr != nil {
			log.Printf("%v\n", priceErr)
			log.Printf("%v\n", amountErr)
			log.Printf("%v\n", ivErr)
			return unpackedOrders, errors.New("error converting string to float64")
		}

		unpackedOrders = append(unpackedOrders, Order{price, amount, iv, exchange})
	}

	return unpackedOrders, nil
}

func mainEventLoop(exchanges Exchanges, connections map[string]ConnData) {
	for {
		if exchanges.Aevo {
			aevoWssRead(connections["aevo"].Ctx, connections["aevo"].Conn)
		}
	}
}

func main() {
	exchanges := Exchanges{Aevo: true, Lyra: false}
	connections := connInit(exchanges)
	if exchanges.Aevo {
		defer connections["aevo"].Cancel()
		defer connections["aevo"].Conn.Close(websocket.StatusNormalClosure, "")
		defer connections["aevo"].Conn.CloseNow()
	}
	if exchanges.Lyra {
		defer connections["lyra"].Cancel()
		defer connections["lyra"].Conn.Close(websocket.StatusNormalClosure, "")
		defer connections["lyra"].Conn.CloseNow()
	}

	mainEventLoop(exchanges, connections)
}
