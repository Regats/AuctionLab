// ========== AUCTION SERVICE ==========
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
	"log"
	"io"
)

type Auction struct {
	ID        int       `json:"id"`
	Item      string    `json:"item"`
	SellerID  int       `json:"seller_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	StartBid  float64   `json:"start_bid"`
	CurrentBid float64 `json:"current_bid"`
	BuyNow    float64   `json:"buy_now,omitempty"`
}

type Bid struct {
	UserID    int       `json:"user_id"`
	AuctionID int       `json:"auction_id"`
	Amount    float64   `json:"amount"`
	Timestamp time.Time `json:"timestamp"`
}

type CreateAuctionRequest struct {
	Item      string    `json:"item"`
	SellerID  int       `json:"seller_id"`
	Duration  int       `json:"duration"` 
	StartBid  float64   `json:"start_bid"`
	BuyNow    float64   `json:"buy_now,omitempty"`
}

var (
	auctions   = make(map[int]Auction)
	bids       = make(map[int][]Bid)
	auctionMutex sync.RWMutex
	nextAuctionID = 1
)

func main() {
	auctionMutex.Lock()
	nextAuctionID = 1
	auctionMutex.Unlock()

	http.HandleFunc("/auctions", createAuctionHandler)
	http.HandleFunc("/auctions/all", getAllAuctionsHandler)
	http.HandleFunc("/auctions/bid", placeBidHandler)
	http.HandleFunc("/auctions/list", listAuctionsHandler)
	
	fmt.Println("Auction Service started on :8081")
	http.ListenAndServe(":8081", nil)
}

func createAuctionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CreateAuctionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid auction data", http.StatusBadRequest)
		return
	}

	checkUserURL := fmt.Sprintf("http://localhost:8080/users/%d", req.SellerID)
	resp, err := http.Get(checkUserURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "Seller not found", http.StatusBadRequest)
		return
	}
	defer resp.Body.Close()

	auctionMutex.Lock()
	defer auctionMutex.Unlock()

	newAuction := Auction{
		ID:        nextAuctionID,
		Item:      req.Item,
		SellerID:  req.SellerID,
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Duration(req.Duration) * time.Hour),
		StartBid:  req.StartBid,
		CurrentBid: req.StartBid,
		BuyNow:    req.BuyNow,
	}

	auctions[nextAuctionID] = newAuction
	nextAuctionID++

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newAuction)
}

func getAllAuctionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	auctionMutex.RLock()
	defer auctionMutex.RUnlock()

	auctionList := make([]Auction, 0, len(auctions))
	for _, auction := range auctions {
		auctionList = append(auctionList, auction)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(auctionList)
}

func placeBidHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newBid Bid
	if err := json.NewDecoder(r.Body).Decode(&newBid); err != nil {
		http.Error(w, "Invalid bid data", http.StatusBadRequest)
		return
	}

	auctionMutex.RLock()
	auction, exists := auctions[newBid.AuctionID]
	auctionMutex.RUnlock()

	if !exists {
		http.Error(w, "Auction not found", http.StatusNotFound)
		return
	}

	if time.Now().After(auction.EndTime) {
		http.Error(w, "Auction has ended", http.StatusForbidden)
		return
	}

	if newBid.Amount <= auction.CurrentBid {
		http.Error(w, "Bid must be higher than current bid", http.StatusBadRequest)
		return
	}

	balanceCheckURL := fmt.Sprintf(
		"http://localhost:8080/users/check_balance?user_id=%d&amount=%f",
		newBid.UserID,
		newBid.Amount,
	)
	
	resp, err := http.Get(balanceCheckURL)
	if err != nil {
		http.Error(w, "User service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Failed to check user balance", http.StatusInternalServerError)
		return
	}

	var balanceResponse struct {
		CanBid  bool    `json:"canBid"`
		Balance float64 `json:"balance"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&balanceResponse); err != nil {
		http.Error(w, "Invalid balance response", http.StatusInternalServerError)
		return
	}

	if !balanceResponse.CanBid {
		http.Error(w, "Insufficient funds", http.StatusPaymentRequired)
		return
	}

	updateURL := "http://localhost:8080/users/update_balance"
    updateData := map[string]interface{}{
        "user_id": newBid.UserID,
        "amount": -newBid.Amount,
    }
    
    jsonData, err := json.Marshal(updateData)
    if err != nil {
        log.Printf("Error marshaling update data: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    log.Printf("Deducting %.2f from user %d", newBid.Amount, newBid.UserID)
    
    req, err := http.NewRequest("PUT", updateURL, bytes.NewBuffer(jsonData))
    if err != nil {
        log.Printf("Error creating request: %v", err)
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: 5 * time.Second}
    respUpdate, err := client.Do(req)
    if err != nil {
        log.Printf("Error sending update request: %v", err)
        http.Error(w, "User service unavailable", http.StatusServiceUnavailable)
        return
    }
    defer respUpdate.Body.Close()

    if respUpdate.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(respUpdate.Body)
        log.Printf("Failed to deduct balance: status %d, response: %s", 
            respUpdate.StatusCode, string(body))
        http.Error(w, "Failed to deduct user balance", http.StatusInternalServerError)
        return
    }

    if respUpdate.StatusCode != http.StatusOK {
        http.Error(w, "Failed to deduct user balance", http.StatusInternalServerError)
        return
    }

	auctionMutex.Lock()
	auction.CurrentBid = newBid.Amount
	auctions[newBid.AuctionID] = auction
	newBid.Timestamp = time.Now()
	bids[newBid.AuctionID] = append(bids[newBid.AuctionID], newBid)
	auctionMutex.Unlock()

	response := struct {
		Status  string  `json:"status"`
		Message string  `json:"message"`
		NewBid  float64 `json:"new_bid"`
	}{
		Status:  "success",
		Message: "Bid accepted",
		NewBid:  newBid.Amount,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func listAuctionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	auctionMutex.RLock()
	defer auctionMutex.RUnlock()

	activeAuctions := []Auction{}
	for _, auction := range auctions {
		if time.Now().Before(auction.EndTime) {
			activeAuctions = append(activeAuctions, auction)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(activeAuctions)
}