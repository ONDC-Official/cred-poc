// Command signreq prints a fresh ONDC Authorization header for manual Postman/curl testing.
//
// Usage:
//
//	go run ./scripts/signreq \
//	  -private-key 'BASE64...' \
//	  -subscriber-id registry.local.test \
//	  -ukid registry-key-1 \
//	  -body '{"cred_id":"ABCDE1234F","cred_type":"PAN"}'
package main

import (
	"flag"
	"fmt"
	"os"

	"credential-service/pkg/ondcauth"
)

func main() {
	privateKey := flag.String("private-key", "", "base64 Ed25519 private key (32-byte seed or 64-byte key)")
	subscriberID := flag.String("subscriber-id", "registry.local.test", "keyId subscriber_id")
	ukID := flag.String("ukid", "registry-key-1", "keyId unique_key_id")
	body := flag.String("body", "", "exact raw request body that will be sent (empty string for GET)")
	flag.Parse()

	if *privateKey == "" {
		fmt.Fprintln(os.Stderr, "missing -private-key")
		os.Exit(1)
	}

	header, err := ondcauth.CreateAuthorizationHeader(ondcauth.CreateAuthorizationHeaderParams{
		Body:                  *body,
		PrivateKey:            *privateKey,
		SubscriberID:          *subscriberID,
		SubscriberUniqueKeyID: *ukID,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(header)
}
