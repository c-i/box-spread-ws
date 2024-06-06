package main

import (
	"math"
	"sync"
	"time"
)

// add mutexes
type Box struct {
	ShortCallBids []Order //K2
	LongCallAsks  []Order //K1
	ShortPutBids  []Order //K1
	LongPutAsks   []Order //K2
	Payoff        float64
	Cost          float64
	Amount        float64
	Profit        float64
	RelProfit     float64
	Apy           float64
}

type BoxKey struct {
	Expiry int64
	K1     float64
	K2     float64
}

type BoxesContainer struct {
	Mu    sync.Mutex
	Boxes map[BoxKey]*Box
}

var BoxContainer = BoxesContainer{Boxes: make(map[BoxKey]*Box)}

func findApy(expiry int64, relProfit float64) float64 {
	expiryTs := float64(expiry)
	now := float64(time.Now().Unix())

	apy := math.Pow(1.0+(relProfit), 365/math.Ceil((1+expiryTs-now)/86400))
	// apy := 365/math.Ceil((1+timestamp-now)/86400) * relProfit

	return apy
}

func updateBox(expiry int64, strikeOrders1 *Orders, strikeOrders2 *Orders) {
	if len(strikeOrders2.CallBids) <= 0 || len(strikeOrders1.CallAsks) <= 0 || len(strikeOrders1.PutBids) <= 0 || len(strikeOrders2.PutAsks) <= 0 {
		return
	}

	bestCallBids := []Order{{Price: -1}}
	bestCallAsks := []Order{{Price: 100000000}}
	bestPutBids := []Order{{Price: -1}}
	bestPutAsks := []Order{{Price: 100000000}}

	for _, order := range strikeOrders2.CallBids {
		if order[0].Price > bestCallBids[0].Price {
			bestCallBids = order
		}
	}
	for _, order := range strikeOrders1.CallAsks {
		if order[0].Price < bestCallAsks[0].Price {
			bestCallAsks = order
		}
	}
	for _, order := range strikeOrders1.PutBids {
		if order[0].Price > bestPutBids[0].Price {
			bestPutBids = order
		}
	}
	for _, order := range strikeOrders2.PutAsks {
		if order[0].Price < bestPutAsks[0].Price {
			bestPutAsks = order
		}
	}

	amount := bestCallBids[0].Amount
	allOrders := [][]Order{bestCallBids, bestCallAsks, bestPutBids, bestPutAsks}
	for _, order := range allOrders {
		if order[0].Amount < amount {
			amount = order[0].Amount
		}
	}

	cost := bestCallAsks[0].Price - bestCallBids[0].Price + bestPutAsks[0].Price - bestPutBids[0].Price
	payoff := strikeOrders2.Strike - strikeOrders1.Strike

	if payoff-cost > 0 {
		key := BoxKey{expiry, strikeOrders1.Strike, strikeOrders2.Strike}
		profit := payoff - cost
		relProfit := profit / cost
		apy := findApy(expiry, relProfit)

		BoxContainer.Boxes[key] = &Box{
			ShortCallBids: bestCallBids,
			LongCallAsks:  bestCallAsks,
			ShortPutBids:  bestPutBids,
			LongPutAsks:   bestPutAsks,
			Payoff:        payoff,
			Cost:          cost,
			Amount:        amount,
			Profit:        profit,
			RelProfit:     relProfit,
			Apy:           apy,
		}
	}
}

func updateBoxes() {
	BoxContainer.Mu.Lock()
	defer BoxContainer.Mu.Unlock()

	for expiry, item := range Orderbooks {
		if len(item) < 2 {
			continue
		}
		for i := 0; i < len(item)-1; i++ {
			for k := i + 1; k < len(item); k++ {
				updateBox(expiry, item[i], item[k])
			}
		}
	}
}
