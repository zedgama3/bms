package disk

import (
	"bytes"
	"crypto/sha256"
	"io"
)

func sum(b []byte) [32]byte { return sha256.Sum256(b) }

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
