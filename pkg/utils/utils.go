package utils

import (
	"os"
	"sync"
)

var (
	mu sync.RWMutex
)

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || os.IsExist(err)
}

func ReadAllText(path string) []byte {
	mu.RLock()
	defer mu.RUnlock()

	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

func WriteAllText(path string, text []byte) {
	mu.Lock()
	defer mu.Unlock()

	err := os.WriteFile(path, text, 0644)
	if err != nil {
		panic(err)
	}
}
