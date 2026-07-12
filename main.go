package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	hashFlag := flag.String("hash", "", "Generate bcrypt hash for the given password and exit")
	flag.Parse()

	if *hashFlag != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*hashFlag), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(hash))
		os.Exit(0)
	}

	listenAddr := envOr("MONITOR_ADDR", ":8088")
	username := envOr("MONITOR_USER", "admin")

	defaultHash, _ := bcrypt.GenerateFromPassword([]byte("changeme"), bcrypt.DefaultCost)
	expectedPassHash := envOr("MONITOR_PASS_HASH", string(defaultHash))

	StartMetricsCollector()
	startServer(listenAddr, username, expectedPassHash)
}
