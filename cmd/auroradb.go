package main

import (
	"auroradb/storage"
	"fmt"
	"log"
)

func main() {
	fmt.Println("Starting AuroraDB Step 1...")

	// Test 1: The Naive In-Place Update
	err := storage.SaveData1("test1.txt", []byte("This is the naive in-place update."))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Successfully ran SaveData1 (In-Place)")

	// Test 2: The Atomic Rename Update
	err = storage.SaveData2("test2.txt", []byte("This is the safe atomic rename update."))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Successfully ran SaveData2 (Atomic Rename)")
}
