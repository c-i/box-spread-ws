package main

type Box struct {
	ShortCallBids []Order //K2
	LongCallAsks  []Order //K1
	ShortPutBids  []Order //K1
	LongPutAsks   []Order //K2
	K1            float64
	K2            float64
	Cost          float64
	Profit        float64
	RelProfit     float64
	Apy           float64
}

// somewhere must check that bids/asks for all 4 legs exist
// func updateBoxes() {
// 	for expiry, item := range Orderbooks{

// 	}
// }
