package compress

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Level 2, JSON documents: a long array keeps its first and last few items,
// every item that signals a failure, every numeric outlier and every item a
// protect pattern matches; each run of dropped items becomes one marker
// string. The reducer works on the compact document's bytes, never
// Unmarshal/Marshal, so the output is valid JSON, member order and number
// spelling are kept, and every kept item is its input bytes. Arrays inside a
// kept item of a reduced array are left whole for the same reason.

const (
	jsonReducerName    = "json"
	jsonReducerVersion = 1
)

// jsonCut is how hard the reducer cuts: an array of at least minItems keeps
// edge items at each end.
type jsonCut struct {
	edge, minItems int
}

// jsonCuts are the reducer's steps. Without a size budget only the first
// applies; a tool's budget (max_unit_tokens) moves down the list until the
// output fits, and the last step is the reducer's floor.
var jsonCuts = []jsonCut{{5, 20}, {5, 13}, {3, 9}, {2, 7}, {1, 5}}

// minJSONCut is the fewest items a cut removes; a run of dropped items
// shorter than its marker is kept instead.
const minJSONCut = 3

// reduceJSON reduces the long arrays of doc, a compact JSON document. With
// budget > 0 (tokens) it cuts harder, step by step, until the output fits.
// It returns doc and false when nothing was cut.
func reduceJSON(doc string, keep func(string) bool, budget int) (string, bool) {
	out, cut := doc, false
	for i, c := range jsonCuts {
		if i > 0 && (budget <= 0 || selectionTokens(out) <= budget) {
			break
		}
		if r, ok := reduceJSONArrays(doc, keep, c); ok {
			out, cut = r, true
		}
	}
	return out, cut
}

// reduceJSONArrays is one step of reduceJSON.
func reduceJSONArrays(doc string, keep func(string) bool, c jsonCut) (string, bool) {
	r := &jsonReducer{doc: doc, keep: keep, cut: c}
	var kept strings.Builder
	if r.value(&kept, 0, 0) != len(doc) || r.items == 0 {
		return doc, false
	}
	r.before, r.after, r.totalItems = selectionTokens(doc), selectionTokens(kept.String()), r.items
	r.markers, r.items, r.runs = true, 0, 0
	var b strings.Builder
	b.Grow(kept.Len() + 128)
	r.value(&b, 0, 0)
	out := b.String()
	if len(out) >= len(doc) || !json.Valid([]byte(out)) {
		return doc, false
	}
	return out, true
}

// jsonReducer walks a compact JSON document, copying it to a builder with
// its long arrays reduced. The first walk leaves the markers out, to count
// the kept size for the first marker; the second writes them.
type jsonReducer struct {
	doc  string
	keep func(string) bool
	cut  jsonCut

	markers       bool
	before, after int
	totalItems    int // items the document cuts, from the first walk
	items, runs   int // items cut and runs written so far
}

// maxJSONReduceDepth is how deep arrays are looked for. Each level reads
// its whole subtree, so the bound keeps a deeply nested document linear.
const maxJSONReduceDepth = 16

// value copies the value at i, depth levels down, and returns where it
// ends.
func (r *jsonReducer) value(b *strings.Builder, i, depth int) int {
	if i >= len(r.doc) {
		return i
	}
	if depth > maxJSONReduceDepth {
		end := skipJSON(r.doc, i)
		b.WriteString(r.doc[i:end])
		return end
	}
	switch r.doc[i] {
	case '{':
		b.WriteByte('{')
		i++
		for i < len(r.doc) && r.doc[i] != '}' {
			end := skipJSON(r.doc, i) // the key
			b.WriteString(r.doc[i:end])
			if end >= len(r.doc) || r.doc[end] != ':' {
				return len(r.doc) + 1
			}
			b.WriteByte(':')
			i = r.value(b, end+1, depth+1)
			if i < len(r.doc) && r.doc[i] == ',' {
				b.WriteByte(',')
				i++
			}
		}
		b.WriteByte('}')
		return i + 1
	case '[':
		items, end := jsonItems(r.doc, i)
		if items == nil {
			b.WriteString(r.doc[i:end])
			return end
		}
		if drop := r.plan(items); drop != nil {
			r.writeReduced(b, items, drop)
			return end
		}
		b.WriteByte('[')
		for k, it := range items {
			if k > 0 {
				b.WriteByte(',')
			}
			r.value(b, it.start, depth+1)
		}
		b.WriteByte(']')
		return end
	default:
		end := skipJSON(r.doc, i)
		b.WriteString(r.doc[i:end])
		return end
	}
}

// writeReduced writes an array with its dropped runs as markers.
func (r *jsonReducer) writeReduced(b *strings.Builder, items []span, drop []bool) {
	b.WriteByte('[')
	first := true
	sep := func() {
		if !first {
			b.WriteByte(',')
		}
		first = false
	}
	for k := 0; k < len(items); {
		if !drop[k] {
			sep()
			b.WriteString(r.doc[items[k].start:items[k].end])
			k++
			continue
		}
		n := 0
		for k < len(items) && drop[k] {
			n++
			k++
		}
		if r.markers {
			sep()
			if r.runs == 0 {
				b.WriteString(encodeJSONString(jsonMarker(r.before, r.after, r.totalItems)))
			} else {
				b.WriteString(encodeJSONString(itemsMarker(n)))
			}
		}
		r.runs++
		r.items += n
	}
	b.WriteByte(']')
}

// plan returns which items of an array go, or nil to keep the array whole.
func (r *jsonReducer) plan(items []span) []bool {
	n := len(items)
	if n < r.cut.minItems {
		return nil
	}
	drop := make([]bool, n)
	for k := r.cut.edge; k < n-r.cut.edge; k++ {
		drop[k] = true
	}
	texts := make([]string, n)
	for k, it := range items {
		texts[k] = r.doc[it.start:it.end]
	}
	for k := range drop {
		if drop[k] && (jsonFailure(texts[k], 0) || (r.keep != nil && r.keep(texts[k]))) {
			drop[k] = false
		}
	}
	for _, k := range jsonOutliers(texts) {
		drop[k] = false
	}
	// A run shorter than its marker stays.
	cut := 0
	for k := 0; k < n; {
		if !drop[k] {
			k++
			continue
		}
		start, size := k, 0
		for k < n && drop[k] {
			size += len(texts[k]) + 1
			k++
		}
		if size <= len(itemsMarker(k-start))+3 {
			for j := start; j < k; j++ {
				drop[j] = false
			}
			continue
		}
		cut += k - start
	}
	if cut < minJSONCut {
		return nil
	}
	return drop
}

// jsonMarker is the first marker a JSON output gets:
//
//	[tyk-compressed: json output, 18,400 → 1,150 tokens; 120 items cut]
func jsonMarker(before, after, items int) string {
	return fmt.Sprintf("[tyk-compressed: %s output, %s → %s tokens; %s cut]", jsonReducerName, thousands(before), thousands(after), itemsNoun(items))
}

// itemsMarker marks a later cut: [tyk-compressed: 12 items].
func itemsMarker(n int) string {
	return "[tyk-compressed: " + itemsNoun(n) + "]"
}

func itemsNoun(n int) string {
	if n == 1 {
		return "1 item"
	}
	return strconv.Itoa(n) + " items"
}

// span is a value's bytes in the document.
type span struct{ start, end int }

// jsonItems returns the item spans of the array at i and where it ends; nil
// items for an empty array.
func jsonItems(doc string, i int) ([]span, int) {
	if i+1 < len(doc) && doc[i+1] == ']' {
		return nil, i + 2
	}
	var items []span
	j := i + 1
	for j < len(doc) {
		end := skipJSON(doc, j)
		items = append(items, span{j, end})
		if end >= len(doc) || doc[end] == ']' {
			return items, end + 1
		}
		j = end + 1 // past the comma
	}
	return items, len(doc)
}

// skipJSON returns where the compact JSON value at i ends.
func skipJSON(doc string, i int) int {
	if i >= len(doc) {
		return i
	}
	switch doc[i] {
	case '"':
		j := i + 1
		for j < len(doc) && doc[j] != '"' {
			if doc[j] == '\\' {
				j++
			}
			j++
		}
		return min(j+1, len(doc))
	case '{', '[':
		depth := 0
		for j := i; j < len(doc); j++ {
			switch doc[j] {
			case '"':
				j = skipJSON(doc, j) - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1
				}
			}
		}
		return len(doc)
	default:
		j := i
		for j < len(doc) && doc[j] != ',' && doc[j] != ']' && doc[j] != '}' && doc[j] != ':' {
			j++
		}
		return j
	}
}

// member is one object member: its decoded key and its value's bytes.
type member struct {
	key, val string
}

// jsonMembers returns an object's members; nil for anything else.
func jsonMembers(obj string) []member {
	if len(obj) < 2 || obj[0] != '{' {
		return nil
	}
	var out []member
	for i := 1; i < len(obj)-1; {
		kend := skipJSON(obj, i)
		if kend >= len(obj) || obj[kend] != ':' {
			return out
		}
		vend := skipJSON(obj, kend+1)
		out = append(out, member{key: jsonKey(obj[i:kend]), val: obj[kend+1 : vend]})
		i = vend + 1
	}
	return out
}

// jsonKey decodes a key, without allocating for the common unescaped one.
func jsonKey(raw string) string {
	if len(raw) >= 2 && strings.IndexByte(raw, '\\') < 0 {
		return raw[1 : len(raw)-1]
	}
	var s string
	_ = json.Unmarshal([]byte(raw), &s)
	return s
}

func jsonString(raw string) (string, bool) {
	if len(raw) < 2 || raw[0] != '"' {
		return "", false
	}
	if strings.IndexByte(raw, '\\') < 0 {
		return raw[1 : len(raw)-1], true
	}
	var s string
	return s, json.Unmarshal([]byte(raw), &s) == nil
}

func jsonNumber(raw string) (float64, bool) {
	if raw == "" || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return 0, false
	}
	f, err := strconv.ParseFloat(raw, 64)
	return f, err == nil && !math.IsInf(f, 0)
}

// Members that say an item failed, by lower-case key.
var (
	// set and not empty, null, false or 0: {"error": "..."}
	jsonErrorKeys = keySet("error", "errors", "err", "exception", "failure", "failures", "fault", "stderr")
	// a failure word, or an object holding one: {"status": "Failed"},
	// {"status": {"phase": "Failed"}}
	jsonStatusKeys = keySet("status", "state", "phase", "level", "severity", "result", "outcome", "conclusion", "health", "reason")
	// an HTTP status of 400 or more: {"status": 503}
	jsonHTTPKeys = keySet("status", "status_code", "statuscode", "http_status", "httpstatus")
	// not zero: {"exit_code": 2}
	jsonExitKeys = keySet("exit_code", "exitcode", "returncode", "return_code", "exit_status")
	// false: {"success": false}
	jsonOKKeys = keySet("success", "ok", "passed", "succeeded", "healthy", "ready")
)

var jsonFailureWord = regexp.MustCompile(`(?i)error|fail|fatal|critical|panic|crash|timeout|timed.?out|denied|reject|unhealthy|abort|degraded|exception|oomkill|evicted|backoff`)

func keySet(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// jsonFailure reports an object item whose members say it failed. depth
// limits the look into a status object to one level.
func jsonFailure(item string, depth int) bool {
	for _, m := range jsonMembers(item) {
		k := strings.ToLower(m.key)
		switch {
		case jsonErrorKeys[k]:
			switch m.val {
			case "null", "false", `""`, "[]", "{}", "0":
			default:
				return true
			}
		case jsonOKKeys[k] && m.val == "false":
			return true
		case jsonExitKeys[k]:
			if f, ok := jsonNumber(m.val); ok && f != 0 {
				return true
			}
		}
		if jsonHTTPKeys[k] {
			if f, ok := jsonNumber(m.val); ok && f >= 400 && f < 600 {
				return true
			}
		}
		if jsonStatusKeys[k] {
			if s, ok := jsonString(m.val); ok && jsonFailureWord.MatchString(s) {
				return true
			}
			if depth == 0 && strings.HasPrefix(m.val, "{") && jsonFailure(m.val, depth+1) {
				return true
			}
		}
	}
	return false
}

// minOutlierSamples is the fewest values a field needs for outliers.
const minOutlierSamples = 8

// jsonOutliers returns the items holding a numeric outlier: a value whose
// modified z-score (median and median absolute deviation) is over 3.5, in
// the items themselves or in a top-level member of object items.
func jsonOutliers(items []string) []int {
	type sample struct {
		item int
		v    float64
	}
	fields := map[string][]sample{}
	var keys []string
	add := func(key string, item int, v float64) {
		if _, ok := fields[key]; !ok {
			keys = append(keys, key)
		}
		fields[key] = append(fields[key], sample{item, v})
	}
	for k, it := range items {
		if v, ok := jsonNumber(it); ok {
			add("", k, v)
			continue
		}
		for _, m := range jsonMembers(it) {
			if v, ok := jsonNumber(m.val); ok {
				add(m.key, k, v)
			}
		}
	}
	var out []int
	for _, key := range keys {
		ss := fields[key]
		if len(ss) < minOutlierSamples {
			continue
		}
		vs := make([]float64, len(ss))
		for i, s := range ss {
			vs[i] = s.v
		}
		med := median(vs)
		dev := make([]float64, len(vs))
		sum := 0.0
		for i, v := range vs {
			dev[i] = math.Abs(v - med)
			sum += dev[i]
		}
		scale := median(dev) / 0.6745
		if scale == 0 {
			scale = 1.253314 * sum / float64(len(dev))
		}
		if scale == 0 {
			continue
		}
		for i, s := range ss {
			if dev[i]/scale > 3.5 {
				out = append(out, s.item)
			}
		}
	}
	return out
}

// median sorts a copy of vs.
func median(vs []float64) float64 {
	s := append([]float64(nil), vs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
