package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/roman-mazur/design-practice-2-template/httptools"
	"github.com/roman-mazur/design-practice-2-template/signal"
)

type sizeTimestamp struct {
	size      int
	timestamp time.Time
}

var (
	port       = flag.Int("port", 8090, "load balancer port")
	timeoutSec = flag.Int("timeout-sec", 3, "request timeout time in seconds")
	https      = flag.Bool("https", false, "whether backends support HTTPs")

	traceEnabled = flag.Bool("trace", false, "whether to include tracing information into responses")
)

var (
	timeout     = time.Duration(*timeoutSec) * time.Second
	serversPool = []string{
		"server1:8080",
		"server2:8080",
		"server3:8080",
	}
	traffic = make(map[string][]sizeTimestamp) // map to keep track of the traffic for each server
	mu      = &sync.Mutex{}                    // mutex to ensure thread safety when updating the traffic map
)

func scheme() string {
	if *https {
		return "https"
	}
	return "http"
}

func health(dst string) bool {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s://%s/health", scheme(), dst), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return true
}

func incrementTraffic(dst string, size int) {
	mu.Lock()
	defer mu.Unlock()

	// Add the size and current time to the queue for this server.
	traffic[dst] = append(traffic[dst], sizeTimestamp{size: size, timestamp: time.Now()})

	// Calculate the total traffic for this server.
	totalTraffic := 0
	for _, st := range traffic[dst] {
		totalTraffic += st.size
	}

	log.Printf("Traffic count for %s: %d", dst, totalTraffic)
}

func reduceTraffic() {
	ticker := time.NewTicker(1 * time.Second) // check every second
	for range ticker.C {
		mu.Lock()
		for server, queue := range traffic {
			// Keep sizes that are less than 5 seconds old.
			newQueue := make([]sizeTimestamp, 0, len(queue))
			cutoff := time.Now().Add(-5 * time.Second)
			for _, st := range queue {
				if st.timestamp.After(cutoff) {
					newQueue = append(newQueue, st)
				}
			}
			traffic[server] = newQueue
		}
		mu.Unlock()
	}
}

func forward(dst string, rw http.ResponseWriter, r *http.Request) error {
	ctx, _ := context.WithTimeout(r.Context(), timeout)
	fwdRequest := r.Clone(ctx)
	fwdRequest.RequestURI = ""
	fwdRequest.URL.Host = dst
	fwdRequest.URL.Scheme = scheme()
	fwdRequest.Host = dst

	resp, err := http.DefaultClient.Do(fwdRequest)
	if err == nil {
		defer resp.Body.Close()

		// Read the response body into a byte slice
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Failed to read response body: %s", err)
			rw.WriteHeader(http.StatusInternalServerError)
			return err
		}

		// Increment the traffic count by the size of the response body
		incrementTraffic(dst, len(bodyBytes))

		for k, values := range resp.Header {
			for _, value := range values {
				rw.Header().Add(k, value)
			}
		}
		if *traceEnabled {
			rw.Header().Set("lb-from", dst)
		}
		log.Println("fwd", resp.StatusCode, resp.Request.URL)
		rw.WriteHeader(resp.StatusCode)
		_, err = rw.Write(bodyBytes) // Write the body bytes to the ResponseWriter
		if err != nil {
			log.Printf("Failed to write response: %s", err)
		}
		return nil
	} else {
		log.Printf("Failed to get response from %s: %s", dst, err)
		rw.WriteHeader(http.StatusServiceUnavailable)
		return err
	}
}

func main() {
	flag.Parse()

	// Initialize traffic for each server as empty queue.
	mu.Lock()
	for _, server := range serversPool {
		traffic[server] = make([]sizeTimestamp, 0)
	}
	mu.Unlock()

	// Launch the goroutine for reducing traffic every second.
	go reduceTraffic()

	for _, server := range serversPool {
		server := server
		go func() {
			for range time.Tick(10 * time.Second) {
				healthy := health(server)
				mu.Lock()
				var load int
				for _, st := range traffic[server] {
					load += st.size
				}
				mu.Unlock()
				log.Printf("%s healthy: %t, load: %d", server, healthy, load)
			}
		}()
	}

	frontend := httptools.CreateServer(*port, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var minTrafficServer string
		minTraffic := int(^uint(0) >> 1) // set to max int value

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

		if minTrafficServer == "" {
			log.Println("No servers available")
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		forward(minTrafficServer, rw, r)
	}))

	log.Println("Starting load balancer...")
	log.Printf("Tracing support enabled: %t", *traceEnabled)
	frontend.Start()
	signal.WaitForTerminationSignal()
}
