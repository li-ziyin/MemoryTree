// Package ids generates and recognises UUIDv7 identifiers as required by the
// MemoryTree format specification.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

// UUIDv7Pattern is the canonical lowercase UUIDv7 form mandated by format v1.
const UUIDv7Pattern = `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`

var uuidV7Re = regexp.MustCompile(UUIDv7Pattern)

// IsUUIDv7 reports whether s is a canonical lowercase UUIDv7.
func IsUUIDv7(s string) bool {
	return uuidV7Re.MatchString(s)
}

// NewUUIDv7 returns a new RFC 9562 UUIDv7. It panics only if the operating
// system fails to supply random bytes, which is not recoverable here.
func NewUUIDv7() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())

	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	if _, err := rand.Read(b[6:]); err != nil {
		panic(fmt.Sprintf("ids: crypto/rand failed: %v", err))
	}

	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant

	var dst [36]byte
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst[:])
}
