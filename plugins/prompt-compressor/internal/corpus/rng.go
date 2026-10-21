package corpus

import (
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"time"
)

// R is a seeded random source. Every corpus entry gets its own, seeded from
// its name, so adding or changing one entry never shifts another.
type R struct{ r *rand.Rand }

// NewR seeds a source from a name.
func NewR(name string) *R {
	h := fnv.New64a()
	h.Write([]byte(name))
	s := h.Sum64()
	return &R{rand.New(rand.NewPCG(s, s^0x9e3779b97f4a7c15))}
}

// Sub derives an independent source for a part of an entry.
func (g *R) Sub(part string) *R { return NewR(fmt.Sprintf("%d/%s", g.r.Uint64(), part)) }

// N returns an int in [lo, hi].
func (g *R) N(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + g.r.IntN(hi-lo+1)
}

// P reports true with probability p.
func (g *R) P(p float64) bool { return g.r.Float64() < p }

// F returns a float in [lo, hi).
func (g *R) F(lo, hi float64) float64 { return lo + g.r.Float64()*(hi-lo) }

// Pick returns a random element.
func Pick[T any](g *R, xs []T) T { return xs[g.r.IntN(len(xs))] }

// Shuffle shuffles xs in place.
func Shuffle[T any](g *R, xs []T) {
	g.r.Shuffle(len(xs), func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })
}

// Hex returns n lowercase hex digits.
func (g *R) Hex(n int) string {
	const digits = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = digits[g.r.IntN(16)]
	}
	return string(b)
}

// Alnum returns n characters from [a-zA-Z0-9].
func (g *R) Alnum(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[g.r.IntN(len(chars))]
	}
	return string(b)
}

// Lower returns n characters from [a-z0-9].
func (g *R) Lower(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[g.r.IntN(len(chars))]
	}
	return string(b)
}

// Bytes returns n random bytes.
func (g *R) Bytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(g.r.IntN(256))
	}
	return b
}

// B64 returns the standard base64 of n random bytes: signatures, opaque
// reasoning payloads. The bytes are random, never a real credential.
func (g *R) B64(n int) string { return base64.StdEncoding.EncodeToString(g.Bytes(n)) }

// UUID returns a random version-4 UUID.
func (g *R) UUID() string {
	b := g.Bytes(16)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// epoch is the fixed "now" of the corpus: timestamps never depend on when
// the generator runs.
var epoch = time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC)

// Time returns a timestamp up to back before the corpus epoch.
func (g *R) Time(back time.Duration) time.Time {
	return epoch.Add(-time.Duration(g.r.Int64N(int64(back))))
}

// Dur returns a duration in seconds formatted like "1.234s".
func (g *R) Secs(lo, hi float64) string { return fmt.Sprintf("%.3fs", g.F(lo, hi)) }

// Words returns n words of plausible engineering prose.
func (g *R) Words(n int) string {
	w := make([]string, n)
	for i := range w {
		w[i] = Pick(g, proseWords)
	}
	return strings.Join(w, " ")
}

// Sentence returns a capitalised sentence of n words.
func (g *R) Sentence(n int) string {
	s := g.Words(n)
	return strings.ToUpper(s[:1]) + s[1:] + "."
}

var proseWords = strings.Fields(`the a an of to in for on with by from as at is are was be
	request response handler service ledger invoice account balance payment refund
	retry timeout cache config value field record batch queue worker job token
	session user tenant region shard index table column row query migration schema
	client server upstream downstream gateway proxy route policy limit budget
	returns rejects accepts validates parses writes reads updates deletes creates
	when if after before until unless while each every all none some only
	error failure warning notice deprecated legacy stable new old current pending
	fast slow large small empty full partial missing duplicate stale fresh valid`)
