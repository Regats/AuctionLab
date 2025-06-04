// ========== USER SERVICE ==========
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
)

type User struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Balance float64 `json:"balance"`
}

var (
	users      = make(map[int]User)
	usersMutex sync.RWMutex
	nextUserID = 1
)

func main() {
	usersMutex.Lock()
	usersMutex.Unlock()

	http.HandleFunc("/users", createUserHandler)
	http.HandleFunc("/users/", getUserHandler)
	http.HandleFunc("/users/all", getAllUsersHandler)
	http.HandleFunc("/users/check_balance", checkBalanceHandler)
	http.HandleFunc("/users/update_balance", updateBalanceHandler)

	fmt.Println("User Service started on :8080")
	http.ListenAndServe(":8080", nil)
}

func createUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newUser User
	if err := json.NewDecoder(r.Body).Decode(&newUser); err != nil {
		http.Error(w, "Invalid user data", http.StatusBadRequest)
		return
	}

	usersMutex.Lock()
	defer usersMutex.Unlock()

	for _, user := range users {
		if user.Email == newUser.Email {
			http.Error(w, "Email already in use", http.StatusConflict)
			return
		}
	}

	newUser.ID = nextUserID
	users[nextUserID] = newUser
	nextUserID++

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newUser)
}

func getUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Path[len("/users/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	usersMutex.RLock()
	user, exists := users[id]
	usersMutex.RUnlock()

	if !exists {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func getAllUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	userList := make([]User, 0, len(users))
	for _, user := range users {
		userList = append(userList, user)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userList)
}

func checkBalanceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, _ := strconv.Atoi(r.URL.Query().Get("user_id"))
	amount, _ := strconv.ParseFloat(r.URL.Query().Get("amount"), 64)

	usersMutex.RLock()
	user, exists := users[userID]
	usersMutex.RUnlock()

	if !exists {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	response := struct {
		CanBid  bool    `json:"canBid"`
		Balance float64 `json:"balance"`
	}{
		CanBid:  user.Balance >= amount,
		Balance: user.Balance,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func updateBalanceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type BalanceUpdate struct {
		UserID int     `json:"user_id"`
		Amount float64 `json:"amount"`
	}

	var update BalanceUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Invalid data", http.StatusBadRequest)
		return
	}

	usersMutex.Lock()
	defer usersMutex.Unlock()

	user, exists := users[update.UserID]
	if !exists {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	if user.Balance+update.Amount < 0 {
		http.Error(w, "Insufficient funds", http.StatusBadRequest)
		return
	}

	user.Balance += update.Amount
	users[update.UserID] = user

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}
