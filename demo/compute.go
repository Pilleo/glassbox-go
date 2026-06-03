package demo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ProcessResult is a complex struct returned by the compute worker.
type ProcessResult struct {
	ID    int
	Hash  string
	Items []string
	Stats map[string]int
}

//gobox:sandbox
type ComputeWorker interface {
	Process(ctx context.Context, iterations int) (*ProcessResult, error)
}

type ComputeWorkerImpl struct{}

func (w *ComputeWorkerImpl) Process(ctx context.Context, iterations int) (*ProcessResult, error) {
	result := &ProcessResult{
		ID:    iterations,
		Items: make([]string, 0, iterations),
		Stats: make(map[string]int),
	}

	for i := 0; i < iterations; i++ {
		// Do some string manipulation
		itemStr := fmt.Sprintf("item-%d-%d", iterations, i)
		result.Items = append(result.Items, itemStr)

		// Do some hashing to simulate compute
		hash := sha256.Sum256([]byte(itemStr))
		hexStr := hex.EncodeToString(hash[:])

		// Some map operations
		if i%2 == 0 {
			result.Stats["even_hashes"]++
		} else {
			result.Stats["odd_hashes"]++
		}

		if i == iterations-1 {
			result.Hash = hexStr
		}
	}

	return result, nil
}
