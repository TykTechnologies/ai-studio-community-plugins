package corpus

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Obj is a JSON object that keeps its keys in insertion order, so generated
// bodies look like what an SDK sends (model first, messages after) rather
// than Go's sorted map order.
type Obj struct{ kv []kv }

type kv struct {
	k string
	v any
}

// O builds an object from alternating keys and values.
func O(pairs ...any) *Obj {
	if len(pairs)%2 != 0 {
		panic("gen.O: odd number of arguments")
	}
	o := &Obj{}
	for i := 0; i < len(pairs); i += 2 {
		o.Set(pairs[i].(string), pairs[i+1])
	}
	return o
}

// Set adds or replaces key k.
func (o *Obj) Set(k string, v any) *Obj {
	for i := range o.kv {
		if o.kv[i].k == k {
			o.kv[i].v = v
			return o
		}
	}
	o.kv = append(o.kv, kv{k, v})
	return o
}

// Get returns the value of key k.
func (o *Obj) Get(k string) any {
	for _, e := range o.kv {
		if e.k == k {
			return e.v
		}
	}
	return nil
}

// Del removes key k.
func (o *Obj) Del(k string) {
	for i, e := range o.kv {
		if e.k == k {
			o.kv = append(o.kv[:i], o.kv[i+1:]...)
			return
		}
	}
}

// Clone copies the object one level deep: nested values are shared, which
// is what session building wants (history is append-only).
func (o *Obj) Clone() *Obj {
	return &Obj{kv: append([]kv(nil), o.kv...)}
}

// Style is how a client serialises JSON.
type Style struct {
	// Spaced writes ", " and ": " like Python's json.dumps defaults.
	Spaced bool
	// ASCII escapes every non-ASCII character as \uXXXX, like Python's
	// ensure_ascii=True.
	ASCII bool
}

// Compact is what the Go, Node and current Python SDKs send.
var Compact = Style{}

// PythonDefault is json.dumps with its default arguments (older httpx,
// LangChain and hand-rolled clients).
var PythonDefault = Style{Spaced: true, ASCII: true}

// Encode serialises v. Values are *Obj, []any, []*Obj, string, bool, nil,
// int, int64, float64 and Raw.
func Encode(v any, s Style) []byte {
	var b strings.Builder
	encode(&b, v, s)
	return []byte(b.String())
}

// Raw is JSON written as is.
type Raw string

func encode(b *strings.Builder, v any, s Style) {
	comma, colon := ",", ":"
	if s.Spaced {
		comma, colon = ", ", ": "
	}
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case Raw:
		b.WriteString(string(t))
	case bool:
		b.WriteString(strconv.FormatBool(t))
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			b.WriteString(strconv.FormatFloat(t, 'f', 1, 64))
		} else {
			b.WriteString(strconv.FormatFloat(t, 'g', -1, 64))
		}
	case string:
		encodeString(b, t, s.ASCII)
	case *Obj:
		b.WriteByte('{')
		for i, e := range t.kv {
			if i > 0 {
				b.WriteString(comma)
			}
			encodeString(b, e.k, s.ASCII)
			b.WriteString(colon)
			encode(b, e.v, s)
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteString(comma)
			}
			encode(b, e, s)
		}
		b.WriteByte(']')
	case []*Obj:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteString(comma)
			}
			encode(b, e, s)
		}
		b.WriteByte(']')
	case []string:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteString(comma)
			}
			encodeString(b, e, s.ASCII)
		}
		b.WriteByte(']')
	default:
		panic(fmt.Sprintf("gen.encode: unsupported %T", v))
	}
}

func encodeString(b *strings.Builder, str string, ascii bool) {
	b.WriteByte('"')
	for i := 0; i < len(str); {
		r, size := utf8.DecodeRuneInString(str[i:])
		i += size
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(b, `\u%04x`, r)
		case r == utf8.RuneError && size == 1:
			panic("gen.encodeString: invalid UTF-8")
		case ascii && r > 0x7f:
			if r > 0xffff {
				r -= 0x10000
				fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			} else {
				fmt.Fprintf(b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}
