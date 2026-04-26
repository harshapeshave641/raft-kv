package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

type Result struct {
	Duration time.Duration
	Err      error
}

func main() {
	url := flag.String("url", "http://localhost:3001/v1/keys/bench", "Target URL")
	requests := flag.Int("n", 1000, "Total number of requests")
	concurrency := flag.Int("c", 10, "Number of concurrent workers")
	method := flag.String("m", "GET", "HTTP method (GET or PUT)")
	flag.Parse()

	results := make(chan Result, *requests)
	var wg sync.WaitGroup

	fmt.Printf("Benchmarking %s with %d requests (%d concurrent)...\n", *url, *requests, *concurrency)

	start := time.Now()

	// Launch workers
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 5 * time.Second}
			for j := 0; j < (*requests / *concurrency); j++ {
				reqStart := time.Now()
				var err error
				if *method == "PUT" {
					body, _ := json.Marshal(map[string]string{"value": "bench-data"})
					req, _ := http.NewRequest("PUT", *url, bytes.NewBuffer(body))
					req.Header.Set("Content-Type", "application/json")
					resp, e := client.Do(req)
					if e != nil {
						err = e
					} else {
						resp.Body.Close()
						if resp.StatusCode >= 400 {
							err = fmt.Errorf("status %d", resp.StatusCode)
						}
					}
				} else {
					resp, e := client.Get(*url)
					if e != nil {
						err = e
					} else {
						resp.Body.Close()
						if resp.StatusCode >= 400 {
							err = fmt.Errorf("status %d", resp.StatusCode)
						}
					}
				}
				results <- Result{Duration: time.Since(reqStart), Err: err}
			}
		}()
	}

	wg.Wait()
	close(results)
	totalDuration := time.Since(start)

	var latencies []time.Duration
	errors := 0
	for r := range results {
		if r.Err != nil {
			errors++
		} else {
			latencies = append(latencies, r.Duration)
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	fmt.Printf("\n--- Results ---\n")
	fmt.Printf("Total time:     %v\n", totalDuration)
	fmt.Printf("Requests/sec:   %.2f\n", float64(len(latencies))/totalDuration.Seconds())
	fmt.Printf("Errors:         %d\n", errors)

	if len(latencies) > 0 {
		fmt.Printf("P50 Latency:    %v\n", latencies[len(latencies)/2])
		fmt.Printf("P95 Latency:    %v\n", latencies[int(float64(len(latencies))*0.95)])
		fmt.Printf("P99 Latency:    %v\n", latencies[int(float64(len(latencies))*0.99)])
	}
}
