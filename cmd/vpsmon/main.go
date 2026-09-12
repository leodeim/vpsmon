package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"vpsmon/internal/api"
	"vpsmon/internal/metrics"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
	skin := envOr("MONITOR_SKIN", "terminal")
	noAuth := envBool("MONITOR_NO_AUTH")

	username := ""
	expectedPassHash := ""
	if !noAuth {
		username = envOr("MONITOR_USER", "admin")
		defaultHash, _ := bcrypt.GenerateFromPassword([]byte("changeme"), bcrypt.DefaultCost)
		expectedPassHash = envOr("MONITOR_PASS_HASH", string(defaultHash))
	}

	metrics.StartCollector()
	api.StartServer(listenAddr, username, expectedPassHash, skin, noAuth)
}
