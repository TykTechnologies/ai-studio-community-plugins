package config

import (
	"strconv"
	"testing"
)

// max_body_bytes is capped: compressing a body allocates about 10-20 times
// its size.
func TestMaxBodyBytesIsBounded(t *testing.T) {
	for v, ok := range map[int]bool{1: true, DefaultMaxBodyBytes: true, MaxMaxBodyBytes: true, MaxMaxBodyBytes + 1: false, 0: false} {
		_, err := ParseJSON([]byte(`{"max_body_bytes": ` + strconv.Itoa(v) + `}`))
		if (err == nil) != ok {
			t.Errorf("max_body_bytes %d: %v", v, err)
		}
	}
}
