package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}

	return hex.EncodeToString(b[:])
}
