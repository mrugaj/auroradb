package db

import (
	"auroradb/storage"
	"log"
)

func OpenDB(loc string) (*DB, func()) {
	kvstore := &storage.KV{Path: loc}
	err := kvstore.Open()
	if err != nil {
		log.Fatalf("creating kvstore: %v", err)
	}
	database := NewDB(loc, kvstore)
	return database, kvstore.Close
}
