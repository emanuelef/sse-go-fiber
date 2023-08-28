package main

import (
	"fmt"
	"math/rand"
	"time"
)

func Filter[T any](filter func(n T) bool) func(T []T) []T {
	return func(list []T) []T {
		r := make([]T, 0, len(list))
		for _, n := range list {
			if filter(n) {
				r = append(r, n)
			}
		}
		return r
	}
}

func main() {
	start := time.Now()
	var vals []int
	for i := 0; i < 5_000_000; i++ {
		vals = append(vals, rand.Intn(100))
	}
	duration := time.Since(start)
	fmt.Println(duration)

	start = time.Now()
	res := Filter(func(val int) bool {
		return val == 7
	})(vals)
	duration = time.Since(start)
	fmt.Println(duration)
	fmt.Println(len(res))
}
