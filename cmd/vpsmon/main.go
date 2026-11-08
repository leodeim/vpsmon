package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"vpsmon/internal/api"
	"github.com/vpsmon/vpsmonlib/metrics"
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

	if spec, ok := os.LookupEnv("MONITOR_TRUSTED_PROXIES"); ok {
		if err := api.SetTrustedProxies(spec); err != nil {
			log.Fatalf("invalid MONITOR_TRUSTED_PROXIES %q: %v", spec, err)
		}
	}

	username := ""
	expectedPassHash := ""
	if !noAuth {
		username = envOr("MONITOR_USER", "admin")
		expectedPassHash = os.Getenv("MONITOR_PASS_HASH")
		if expectedPassHash == "" {
			log.Fatal("MONITOR_PASS_HASH is required: generate one with 'vpsmon -hash <password>', or set MONITOR_NO_AUTH=true if access is protected another way")
		}
	}

	metrics.StartCollector()
	api.StartServer(listenAddr, username, expectedPassHash, skin, noAuth)
}
