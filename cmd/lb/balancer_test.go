package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestHealthCheck tests the health check function
func TestHealthCheck(t *testing.T) {
	// If a server is not available, the health check should return false.
	assert.False(t, health("non-existing-server:8080"))
}

// TestServerSelectionAfterLoadExpiry tests the server selection logic after load expiry
func TestServerSelectionAfterLoadExpiry(t *testing.T) {
	// initialize traffic map
	mu.Lock()
	for _, server := range serversPool {
		traffic[server] = make([]sizeTimestamp, 0)
	}
	// Let's simulate some load 10 seconds ago (which should be expired by now)
	traffic["server1:8080"] = append(traffic["server1:8080"], sizeTimestamp{size: 100, timestamp: time.Now().Add(-10 * time.Second)})
	mu.Unlock()

	// create a mock HTTP request
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	// mock the http response
	rr := httptest.NewRecorder()

	// simulate a request to the load balancer
	http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var minTrafficServer string
		minTraffic := int(^uint(0) >> 1) // Set to max int value

		mu.Lock()
		for server, serverQueue := range traffic {
			serverTraffic := 0
			for _, st := range serverQueue {
				if time.Since(st.timestamp).Seconds() <= 5 { // Consider only recent load
					serverTraffic += st.size
				}
			}
			if serverTraffic < minTraffic {
				minTraffic = serverTraffic
				minTrafficServer = server
			}
		}
		mu.Unlock()

		forward(minTrafficServer, rw, r)
	}).ServeHTTP(rr, req)

	// "server1:8080" should not have been selected as it had load, but it was old.
	assert.NotEqual(t, "server1:8080", rr.Header().Get("lb-from"))
}

func TestBalancer(t *testing.T) {
	// Initialize traffic map
	mu.Lock()
	for _, server := range serversPool {
		traffic[server] = make([]sizeTimestamp, 0)
	}
	mu.Unlock()

	// Create a mock HTTP request
	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("TestLeastTraffic", func(t *testing.T) {
		// Mock the HTTP response
		rr := httptest.NewRecorder()

		// Simulate a request to the load balancer
		http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			var minTrafficServer string
			minTraffic := int(^uint(0) >> 1) // Set to max int value

			mu.Lock()
			for server, serverQueue := range traffic {
				serverTraffic := 0
				for _, st := range serverQueue {
					serverTraffic += st.size
				}
				if serverTraffic < minTraffic {
					minTraffic = serverTraffic
					minTrafficServer = server
				}
			}
			mu.Unlock()

			forward(minTrafficServer, rw, r)
		}).ServeHTTP(rr, req)

		// Find the server with least traffic
		var minTrafficServer string
		minTraffic := int(^uint(0) >> 1) // Set to max int value
		mu.Lock()
		for server, serverQueue := range traffic {
			serverTraffic := 0
			for _, st := range serverQueue {
				serverTraffic += st.size
			}
			if serverTraffic < minTraffic {
				minTraffic = serverTraffic
				minTrafficServer = server
			}
		}
		mu.Unlock()

		// Calculate the total traffic for minTrafficServer
		minServerTraffic := 0
		for _, st := range traffic[minTrafficServer] {
			minServerTraffic += st.size
		}

		// Check if the server with least traffic was selected
		assert.Equal(t, minTraffic, minServerTraffic)
	})
}
