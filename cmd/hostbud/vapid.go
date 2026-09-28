package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// printVAPIDKeys prints a fresh VAPID key pair as .env lines (make
// vapid-keys). It writes no file: the owner copies the lines into .env.
func printVAPIDKeys(w io.Writer) error {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "HOSTBUD_VAPID_PUBLIC_KEY=%s\nHOSTBUD_VAPID_PRIVATE_KEY=%s\n",
		base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(k.Bytes()))
	return err
}
