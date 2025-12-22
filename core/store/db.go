package store

import (
	"log"

	"github.com/dgraph-io/badger/v4"
)

var DB *badger.DB

func Init(path string) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil // Disable verbose logging

	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	DB = db
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}
