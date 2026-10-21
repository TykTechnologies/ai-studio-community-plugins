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

// max_in_flight_bytes is bounded, and never below max_body_bytes, whose
// largest bodies would otherwise never fit.
func TestMaxInFlightBytesIsBounded(t *testing.T) {
	for _, tc := range []struct {
		cfg string
		ok  bool
	}{
		{`{}`, true},
		{`{"max_in_flight_bytes": 4194304}`, true},
		{`{"max_in_flight_bytes": 268435456}`, true},
		{`{"max_in_flight_bytes": 268435457}`, false},
		{`{"max_in_flight_bytes": 0}`, false},
		{`{"max_in_flight_bytes": 4194303}`, false},
		{`{"max_body_bytes": 16777216, "max_in_flight_bytes": 16777216}`, true},
		{`{"max_body_bytes": 16777216, "max_in_flight_bytes": 8388608}`, false},
	} {
		c, err := ParseJSON([]byte(tc.cfg))
		if (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.cfg, err)
		}
		if tc.cfg == `{}` && c.MaxInFlightBytes != DefaultMaxInFlightBytes {
			t.Errorf("default %d", c.MaxInFlightBytes)
		}
	}
}
