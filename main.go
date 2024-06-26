package main

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

//seperate slices for each exchange? [][]Order

// orderbook data received from exchange comes in a 2d array structured: [[price, amount, IV (if applicable)]...], Order is one innermost item (array) of this array
type Order struct {
	Price      float64
	Amount     float64
	Iv         float64
	Strike     float64
	OptionType string
	Exchange   string
}

type Orders struct {
	CallBids map[string][]Order //exchange: []Order
	CallAsks map[string][]Order
	PutBids  map[string][]Order
	PutAsks  map[string][]Order
	Strike   float64
}

type Exchanges struct {
	Aevo bool
	Lyra bool
}

// expiry: strike: exchange: orderbook
var Orderbooks = make(map[int64][]*Orders) //Orders sorted by strike

func updateExistingOrderbook(order *Orders, bids []Order, asks []Order, exchange string, optionType string) {
	_, callBidsExists := order.CallBids[exchange]
	_, callAsksExists := order.CallAsks[exchange]
	_, putBidsExists := order.PutBids[exchange]
	_, putAsksExists := order.PutAsks[exchange]
	if !callBidsExists || !callAsksExists || !putBidsExists || !putAsksExists {
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

func updateOrderbook(expiry int64, bids []Order, asks []Order) {
	_, exists := Orderbooks[expiry]
	// remember to sort by strike
	if !exists { //might be unnecessary
		Orderbooks[expiry] = make([]*Orders, 0)
	}

	var strike float64
	var exchange string
	var optionType string

	if len(bids) > 0 {
		strike = bids[0].Strike
		exchange = bids[0].Exchange
		optionType = bids[0].OptionType
	} else {
		strike = asks[0].Strike
		exchange = asks[0].Exchange
		optionType = asks[0].OptionType
	}

	strikeExists := false

	for _, order := range Orderbooks[expiry] {
		if order.Strike == strike {

			updateExistingOrderbook(order, bids, asks, exchange, optionType)

			// fmt.Printf("%+v\n\n", order)

			strikeExists = true
			break
		}
	}

	if !strikeExists {

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

func unpackOrders(orders []interface{}, strike float64, optionType string, exchange string) ([]Order, error) {
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

		unpackedOrders = append(unpackedOrders, Order{price, amount, iv, strike, optionType, exchange})
	}

	return unpackedOrders, nil
}

func mainEventLoop(exchanges Exchanges, connections map[string]ConnData) {
	for {
		if exchanges.Aevo {
			aevoWssRead(connections["aevo"].Ctx, connections["aevo"].Conn)
		}
		if exchanges.Lyra {
			lyraWssRead(connections["lyra"].Ctx, connections["lyra"].Conn)
		}
		updateBoxes()
	}
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("templates/index.html"))
	tmpl.Execute(w, nil)
}

func boxTableHandler(w http.ResponseWriter, r *http.Request) {
	BoxContainer.Mu.Lock()
	defer BoxContainer.Mu.Unlock()
	boxTablesSlice := make([]*Box, len(BoxContainer.Boxes)) //converting to slice to sort by apy
	keySlice := make([]BoxKey, len(BoxContainer.Boxes))
	i := 0
	for key, table := range BoxContainer.Boxes {
		keySlice[i] = key
		boxTablesSlice[i] = table
		i++
	}
	sort.Slice(boxTablesSlice, func(i, j int) bool { return boxTablesSlice[i].Apy > boxTablesSlice[j].Apy })

	responseStr := ""
	for i, value := range boxTablesSlice {
		expiryUnix := time.Unix(keySlice[i].Expiry, 0)
		expiry := strings.ToUpper(expiryUnix.Format("02Jan06 15:04:05"))

		responseStr += fmt.Sprintf(
			`<tr>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			<td>%s</td>
			</tr>`,
			expiry,
			strconv.FormatFloat(keySlice[i].K1, 'f', 3, 64),
			strconv.FormatFloat(keySlice[i].K2, 'f', 3, 64),
			value.ShortCallBids[0].Exchange,
			strconv.FormatFloat(value.ShortCallBids[0].Price, 'f', 3, 64),
			value.LongCallAsks[0].Exchange,
			strconv.FormatFloat(value.LongCallAsks[0].Price, 'f', 3, 64),
			value.ShortPutBids[0].Exchange,
			strconv.FormatFloat(value.ShortPutBids[0].Price, 'f', 3, 64),
			value.LongPutAsks[0].Exchange,
			strconv.FormatFloat(value.LongPutAsks[0].Price, 'f', 3, 64),
			strconv.FormatFloat(value.Cost, 'f', 3, 64),
			strconv.FormatFloat(value.Payoff, 'f', 3, 64),
			strconv.FormatFloat(value.Amount, 'f', 3, 64),
			strconv.FormatFloat(value.Profit, 'f', 3, 64),
			strconv.FormatFloat(value.RelProfit*100, 'f', 3, 64),
			strconv.FormatFloat(value.Apy, 'f', 3, 64),
		)
	}

	fmt.Fprint(w, responseStr)
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

	go mainEventLoop(exchanges, connections)

	http.HandleFunc("/", serveHome)
	http.HandleFunc("/update-table", boxTableHandler)
	fmt.Println("Server starting on http://localhost:8081...")
	log.Fatal(http.ListenAndServe(":8081", nil))
}
