package util

import (
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
)

type Set struct {
	store map[string]struct{}
}

func NewSet() *Set {
	return &Set{store: make(map[string]struct{})}
}

func (s *Set) Has(key []byte) bool {
	_, ok := s.store[string(key)]
	return ok
}

func (s *Set) Set(key []byte) bool {
	k := string(key)
	if _, ok := s.store[k]; ok {
		return false
	}
	s.store[k] = struct{}{}
	return true
}

func (s *Set) Del(key []byte) bool {
	k := string(key)
	if _, ok := s.store[k]; !ok {
		return false
	}
	delete(s.store, k)
	return true
}

func NewTempFileLoc() string {
	id, err := uuid.NewRandom()
	if err != nil {
		log.Fatalf("getting new uuid: %s", err)
	}
	return fmt.Sprintf("/tmp/%s.db", id.String())
}

func IntSliceRemove(key uint64, ls []uint64) []uint64 {
	for i := 0; i < len(ls); i++ {
		if ls[i] == key {
			ls = append(ls[:i], ls[i+1:]...)
			i--
		}
	}
	return ls
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
