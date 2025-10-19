package main

import (
	"context"
	"log"
	"runtime"
	"sync"
)

// ProcessInParallel processes items in parallel with a configurable number of workers.
// It takes a slice of input items, a processing function, and the maximum number of workers.
// The processing function should take an input item and return a result and an error.
// Returns a slice of results in the same order as the input.
func ProcessInParallel[T any, R any](
	ctx context.Context,
	items []T,
	processFunc func(T) (R, error),
	maxWorkers int,
) ([]R, []error) {
	if len(items) == 0 {
		return nil, nil
	}

	// Use the smaller of maxWorkers or the number of CPU cores
	if maxWorkers <= 0 || maxWorkers > runtime.NumCPU()*2 {
		maxWorkers = runtime.NumCPU() * 2
	}
	if maxWorkers > len(items) {
		maxWorkers = len(items)
	}

	// Create channels for work distribution
	jobs := make(chan T, len(items))
	results := make(chan struct {
		index int
		item  T
		value R
		err   error
	}, len(items))

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
					result, err := processFunc(item)
					results <- struct {
						index int
						item  T
						value R
						err   error
					}{
						item:  item,
						value: result,
						err:   err,
					}
				}
			}
		}()
	}

	// Send jobs to workers
	for _, item := range items {
		select {
		case jobs <- item:
		case <-ctx.Done():
			close(jobs)
			return nil, []error{ctx.Err()}
		}
	}
	close(jobs)

	// Close results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	resultSlice := make([]R, len(items))
	errors := make([]error, 0, len(items))

	for result := range results {
		if result.err != nil {
			log.Printf("Error processing item %v: %v", result.item, result.err)
			errors = append(errors, result.err)
			continue
		}
		// Find the index of the item in the original slice
		for i, item := range items {
			if any(item) == any(result.item) {
				resultSlice[i] = result.value
				break
			}
		}
	}

	return resultSlice, errors
}