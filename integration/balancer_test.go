package integration

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const baseAddress = "http://balancer:8090"

var client = http.Client{
	Timeout: 3 * time.Second,
}

func TestBalancer(t *testing.T) {
	if _, exists := os.LookupEnv("INTEGRATION_TEST"); !exists {
		t.Skip("Integration test is not enabled")
	}

	// Map to count the total data load that each server handles.
	serverLoads := make(map[string]int)
	var serversPool []string

	// Send several requests to the load balancer.
	numRequests := 100
	for i := 0; i < numRequests; i++ {
		resp, err := client.Get(fmt.Sprintf("%s/api/v1/some-data", baseAddress))
		if err != nil {
			t.Error(err)
			continue
		}
		defer resp.Body.Close()

		// Get the server which handled the request.
		server := resp.Header.Get("lb-from")

		// Get the size of the data processed by the server.
		dataSizeStr := resp.Header.Get("data-size")
		dataSize, err := strconv.Atoi(dataSizeStr)
		if err != nil {
			t.Errorf("Failed to convert data size to int: %s", err)
			continue
		}

		// Add the data size to the load of this server.
		serverLoads[server] += dataSize

		// Extract the server pool from the headers.
		if i == 0 {
			serversPool = strings.Split(resp.Header.Get("server-pool"), ",")
		}

		// Log which server handled the request and the current loads.
		t.Logf("Response from [%s], loads: %v", server, serverLoads)
	}

	// Check if the load is spread approximately equally between the servers.
	avgLoad := 0
	for _, load := range serverLoads {
		avgLoad += load
	}
	avgLoad /= len(serversPool)

	// Use a tolerance of 10%.
	tolerance := float64(avgLoad) * 0.1

	for _, load := range serverLoads {
		if math.Abs(float64(load-avgLoad)) > tolerance {
			t.Errorf("The load is not spread equally: %v", serverLoads)
			break
		}
	}

	// Print a summary of the test data.
	totalLoad := 0
	for _, load := range serverLoads {
		totalLoad += load
	}
	t.Logf("Test Summary: \nNumber of servers: %d\nTotal load processed: %d\nAverage load per server: %d\n", len(serversPool), totalLoad, avgLoad)
}

func BenchmarkBalancer(b *testing.B) {
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(fmt.Sprintf("%s/api/v1/some-data", baseAddress))
		if err != nil {
			b.Error(err)
		}
		defer resp.Body.Close()
	}
}
