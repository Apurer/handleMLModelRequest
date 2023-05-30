package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

type User struct {
	Newest     uuid.UUID
	NewestMx   sync.RWMutex
	Busy       bool
	BusyMx     sync.RWMutex
	Cond       *sync.Cond
	LastActive time.Time
}

var (
	users   = make(map[string]*User)
	usersMx = &sync.Mutex{} // Mutex for the users map
)

const (
	sleepTime       = 20 * time.Second
	cleanupInterval = 10 * time.Minute
	userTimeout     = 10 * time.Minute
)

func main() {
	http.HandleFunc("/", handleRequest)
	go cleanupInactiveUsers() // start the cleanup goroutine
	fmt.Println("Starting server on :8080")
	http.ListenAndServe(":8080", nil)
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Missing API Key")
		return
	}

	usersMx.Lock()
	user, exists := users[apiKey]
	if !exists {
		user = &User{
			Cond: sync.NewCond(&sync.Mutex{}),
		}
		users[apiKey] = user
	}
	usersMx.Unlock()

	user.NewestMx.Lock()
	user.Newest = uuid.New()
	current := user.Newest
	user.NewestMx.Unlock()

	user.Cond.L.Lock()
	user.Cond.Broadcast()
	user.Cond.L.Unlock()

	executeMLModelRequest(user, current, w, r)
}

func executeMLModelRequest(user *User, current uuid.UUID, w http.ResponseWriter, r *http.Request) {
	user.BusyMx.RLock()
	busy := user.Busy
	user.BusyMx.RUnlock()

	if busy {
		user.Cond.L.Lock()
		user.Cond.Wait()
		user.Cond.L.Unlock()

		user.NewestMx.RLock()
		newest := user.Newest
		user.NewestMx.RUnlock()

		if current != newest {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "Skipped")
			return
		}

		executeMLModelRequest(user, current, w, r)
	} else {
		user.BusyMx.Lock()
		user.Busy = true
		user.BusyMx.Unlock()

		time.Sleep(sleepTime)

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")

		user.BusyMx.Lock()
		user.Busy = false
		user.BusyMx.Unlock()

		user.Cond.L.Lock()
		user.Cond.Broadcast()
		user.Cond.L.Unlock()
	}
}

func cleanupInactiveUsers() {
	for range time.Tick(cleanupInterval) {
		usersMx.Lock()
		for apiKey, user := range users {
			if time.Since(user.LastActive) > userTimeout {
				delete(users, apiKey)
			}
		}
		usersMx.Unlock()
	}
}
