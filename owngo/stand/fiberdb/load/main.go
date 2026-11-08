// Command load drives the fiberdb stand: c clients send GET url in a loop for
// d and the command prints throughput, p50, p99 and the error count.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("load", flag.ContinueOnError)
	url := fs.String("url", "http://127.0.0.1:8080/items/1", "URL to GET")
	clients := fs.Int("c", 100, "concurrent clients, each on its own connection")
	d := fs.Duration("d", 10*time.Second, "duration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *clients < 1 || *d <= 0 {
		return errors.New("-c must be at least 1 and -d positive")
	}

	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConnsPerHost = *clients
	t.MaxConnsPerHost = *clients
	client := &http.Client{Timeout: 10 * time.Second, Transport: t}

	ctx, cancel := context.WithTimeout(context.Background(), *d)
	defer cancel()
	lat := make([][]time.Duration, *clients)
	fails := make([]int, *clients)
	var wg sync.WaitGroup
	start := time.Now()
	for i := range *clients {
		wg.Go(func() {
			for ctx.Err() == nil {
				t0 := time.Now()
				ok := get(ctx, client, *url)
				if ctx.Err() != nil {
					return // the request the deadline cut is not a sample
				}
				if !ok {
					fails[i]++
					continue
				}
				lat[i] = append(lat[i], time.Since(t0))
			}
		})
	}
	wg.Wait()
	elapsed := time.Since(start)

	all := slices.Sorted(slices.Values(slices.Concat(lat...)))
	errs := 0
	for _, n := range fails {
		errs += n
	}
	if len(all) == 0 {
		return fmt.Errorf("no successful requests, %d errors", errs)
	}
	q := func(p float64) time.Duration { return all[int(p*float64(len(all)-1))] }
	fmt.Printf("clients=%d requests=%d errors=%d rps=%.0f p50=%v p99=%v max=%v\n",
		*clients, len(all), errs, float64(len(all))/elapsed.Seconds(), q(0.50), q(0.99), all[len(all)-1])
	return nil
}

// get reports whether GET url answered 200 with a body read to the end.
func get(ctx context.Context, client *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err == nil && resp.StatusCode == http.StatusOK
}
