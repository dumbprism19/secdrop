// Package secret holds plaintext in a type that refuses to print itself.
package secret

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
)

const redacted = "[redacted]"

// Value is a plaintext secret. Formatting, logging or marshaling it yields
// "[redacted]"; the only way to read it is Reveal or Bytes.
type Value struct{ b []byte }

func New(b []byte) Value {
	c := make([]byte, len(b))
	copy(c, b)
	return Value{b: c}
}

func FromString(s string) Value { return Value{b: []byte(s)} }

func (v Value) Reveal() string { return string(v.b) }
func (v Value) Bytes() []byte  { return v.b }
func (v Value) Len() int       { return len(v.b) }
func (v Value) IsZero() bool   { return len(v.b) == 0 }

func (v Value) Equal(o Value) bool { return subtle.ConstantTimeCompare(v.b, o.b) == 1 }

// Wipe overwrites the backing bytes. Copies made with Reveal are not affected.
func (v *Value) Wipe() {
	for i := range v.b {
		v.b[i] = 0
	}
	v.b = nil
}

func (Value) String() string               { return redacted }
func (Value) GoString() string             { return redacted }
func (Value) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
func (Value) MarshalText() ([]byte, error) { return []byte(redacted), nil }
func (Value) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(redacted)) }

// sessionKey is generated once per process and never leaves memory, so
// fingerprints can't be matched across runs or brute-forced from a screenshot.
var sessionKey = func() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		panic("secret: no entropy for session key")
	}
	return k
}()

// Fingerprint is a short HMAC-SHA256 of the value under the session key.
func (v Value) Fingerprint() string {
	m := hmac.New(sha256.New, sessionKey)
	m.Write(v.b)
	s := hex.EncodeToString(m.Sum(nil)[:4])
	return s[:4] + "·" + s[4:]
}
