package records

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// KV is the part of the plugin KV service the store uses. Read must return
// an error that notFound recognises for a missing key.
type KV interface {
	Read(ctx context.Context, key string) ([]byte, error)
	Write(ctx context.Context, key string, value []byte, expireAt *time.Time) (bool, error)
	Delete(ctx context.Context, key string) (bool, error)
}

// Store keeps Studio's hourly rollups in plugin KV.
//
// KV has no List, so every document is reachable from an index:
//
//	pc:edges                     edges that sent records (hashed ids)
//	pc:e:<edge>:hours            hour buckets held for one edge
//	pc:e:<edge>:h:<hour>         that edge's rows for one hour
//	pc:policies                  when each policy hash was first and last seen
//
// Documents are per edge: an edge's payloads arrive one batch at a time,
// on one Studio replica at a time, so two replicas never merge into the
// same hour document. Each hour document lists the batch IDs merged into
// it, which makes applying a batch idempotent: a redelivered batch (edge
// retry, hub restart, a new cluster leader replaying) changes nothing.
//
// Keys are built from hashes and integers only, never from edge-supplied
// strings.
type Store struct {
	kv        KV
	notFound  func(error) bool
	retention time.Duration
	// batchIDsPerHour bounds the idempotency list kept in an hour document.
	batchIDsPerHour int
	// rowsPerHour and policyMarkers bound what an edge can make Studio
	// keep, whatever its batches say.
	rowsPerHour   int
	policyMarkers int
	// hourDocBytes is the size past which an hour document takes no new
	// rows or names (KV values are capped at 5 MB).
	hourDocBytes int
	now          func() time.Time

	mu    sync.Mutex
	stats StoreStats
}

// Store defaults.
const (
	DefaultRetention = 90 * 24 * time.Hour
	// maxBatchIDsPerHour bounds the idempotency list in an hour document.
	// An edge flushes about once a minute.
	maxBatchIDsPerHour = 1000
	// MaxEdges bounds the edge index.
	MaxEdges = 1000
	// MaxRowsPerHour bounds the rows in one edge's hour document; later
	// rows fold into an overflow row per outcome.
	MaxRowsPerHour = 5000
	// MaxPolicyMarkers bounds the policy markers; the least recently seen
	// go first.
	MaxPolicyMarkers = 500
	// MaxHourDocBytes is an hour document's size budget, under the 5 MB KV
	// value cap with room for one batch's growth in existing rows.
	MaxHourDocBytes = 4 << 20
	// maxClockSkew is how far ahead of Studio's clock an edge's hour may be.
	maxClockSkew = time.Hour
)

// NewStore returns a store over kv.
func NewStore(kv KV, notFound func(error) bool) *Store {
	return &Store{
		kv: kv, notFound: notFound, retention: DefaultRetention, batchIDsPerHour: maxBatchIDsPerHour,
		rowsPerHour: MaxRowsPerHour, policyMarkers: MaxPolicyMarkers, hourDocBytes: MaxHourDocBytes, now: time.Now,
	}
}

// EdgeKey is the KV-safe name of an edge.
func EdgeKey(namespace, edgeID string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + edgeID))
	return hex.EncodeToString(sum[:8])
}

type hourDoc struct {
	Batches []string          `json:"batches"`
	Rows    map[string]*Delta `json:"rows"`
	// Folded counts deltas merged into the overflow rows.
	Folded int64 `json:"folded,omitempty"`
}

// overflowRow names the row an hour folds new rows into once it holds
// MaxRowsPerHour rows. It never collides with a key's Row.
func overflowRow(outcome string) string { return "overflow:" + outcome }

type edgesDoc struct {
	Edges map[string]int64 `json:"edges"` // edge key -> last hour seen
}

// PolicyMarker records when a policy version was first seen.
type PolicyMarker struct {
	Hash      string `json:"hash"`
	FirstSeen int64  `json:"first_seen"` // Unix hour
	LastSeen  int64  `json:"last_seen"`  // Unix hour
	Name      string `json:"name,omitempty"`
	Level     int    `json:"level"`
}

func (s *Store) read(ctx context.Context, key string, v interface{}) (bool, error) {
	raw, err := s.kv.Read(ctx, key)
	if err != nil {
		if s.notFound != nil && s.notFound(err) {
			return false, nil
		}
		return false, err
	}
	if len(raw) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return true, nil
}

func (s *Store) write(ctx context.Context, key string, v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.kv.Write(ctx, key, raw, nil)
	return err
}

func hoursKey(edge string) string            { return "pc:e:" + edge + ":hours" }
func hourKey(edge string, hour int64) string { return fmt.Sprintf("pc:e:%s:h:%d", edge, hour) }

// Apply merges a batch from an edge. It validates the batch first and
// stores nothing from an invalid one. Applying the same batch again is a
// no-op.
func (s *Store) Apply(ctx context.Context, namespace, edgeID string, b *Batch) error {
	if err := b.Validate(); err != nil {
		return err
	}
	edge := EdgeKey(namespace, edgeID)
	oldest := HourOf(s.now().Add(-s.retention))

	s.mu.Lock()
	defer s.mu.Unlock()

	// Hours are the edge's word: one past retention would never be kept,
	// one in the future would never age out. A small skew allows for
	// clocks and the current hour.
	newest := HourOf(s.now().Add(maxClockSkew))
	byHour := map[int64][]*Delta{}
	for _, d := range b.Deltas {
		switch {
		case d.Hour < oldest:
			s.stats.TooOld++
			continue
		case d.Hour > newest:
			s.stats.Future++
			continue
		}
		byHour[d.Hour] = append(byHour[d.Hour], d)
	}
	hours := make([]int64, 0, len(byHour))
	for h := range byHour {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })

	// Index first: a crash after it leaves an hour listed with no or old
	// rows, which reads as empty; never rows no index reaches.
	if err := s.indexHours(ctx, edge, hours, oldest); err != nil {
		return err
	}
	for _, h := range hours {
		if err := s.mergeHour(ctx, edge, h, b.ID, byHour[h]); err != nil {
			return err
		}
	}
	if len(hours) > 0 {
		if err := s.indexEdge(ctx, edge, hours[len(hours)-1]); err != nil {
			return err
		}
	}
	var kept []*Delta
	for _, h := range hours {
		kept = append(kept, byHour[h]...)
	}
	return s.markPolicies(ctx, kept, oldest)
}

// StoreStats counts what Apply left out.
type StoreStats struct {
	// TooOld and Future count deltas outside retention or beyond the clock
	// skew; they are not stored.
	TooOld int64 `json:"too_old"`
	Future int64 `json:"future"`
	// Folded counts deltas merged into an hour's overflow row because the
	// hour already held MaxRowsPerHour rows.
	Folded int64 `json:"folded"`
}

// Stats returns the counts since the store was made.
func (s *Store) Stats() StoreStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

func (s *Store) mergeHour(ctx context.Context, edge string, hour int64, batch string, deltas []*Delta) error {
	key := hourKey(edge, hour)
	raw, err := s.kv.Read(ctx, key)
	if err != nil && !(s.notFound != nil && s.notFound(err)) {
		return err
	}
	load := func() (hourDoc, error) {
		doc := hourDoc{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &doc); err != nil {
				return doc, fmt.Errorf("%s: %w", key, err)
			}
		}
		if doc.Rows == nil {
			doc.Rows = map[string]*Delta{}
		}
		return doc, nil
	}
	doc, err := load()
	if err != nil {
		return err
	}
	for _, id := range doc.Batches {
		if id == batch {
			return nil // already applied
		}
	}

	// A document at its byte budget takes no new rows, strategy names or
	// currencies: those fold into the overflow rows and the "other" names,
	// so totals stay right and the document stops growing. A batch that
	// would take it past the budget is merged again that way.
	full := len(raw) >= s.hourDocBytes
	folded := s.mergeRows(&doc, batch, deltas, full)
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if !full && len(out) > s.hourDocBytes {
		if doc, err = load(); err != nil {
			return err
		}
		folded = s.mergeRows(&doc, batch, deltas, true)
		if out, err = json.Marshal(doc); err != nil {
			return err
		}
	}
	s.stats.Folded += folded
	_, err = s.kv.Write(ctx, key, out, nil)
	return err
}

// mergeRows merges deltas into doc and records the batch. Past
// rowsPerHour rows, or with full, new rows fold into an overflow row per
// outcome; with full, merges add no new map keys either. It returns the
// number of deltas folded.
func (s *Store) mergeRows(doc *hourDoc, batch string, deltas []*Delta, full bool) int64 {
	var folded int64
	merge := func(dst, src *Delta) {
		if full {
			dst.mergeKnown(src)
		} else {
			dst.Merge(src)
		}
	}
	for _, d := range deltas {
		row := d.Row()
		if cur := doc.Rows[row]; cur != nil {
			merge(cur, d)
			continue
		}
		if full || len(doc.Rows) >= s.rowsPerHour {
			folded++
			doc.Folded++
			row = overflowRow(d.Outcome)
			if cur := doc.Rows[row]; cur != nil {
				merge(cur, d)
				continue
			}
			cp := NewDelta(Key{Hour: d.Hour, Outcome: d.Outcome})
			cp.Overflow = true
			cp.mergeKnown(d)
			cp.PolicyName, cp.Mode, cp.Level = "", "", 0
			doc.Rows[row] = cp
			continue
		}
		cp := NewDelta(d.Key)
		cp.Merge(d)
		doc.Rows[row] = cp
	}
	doc.Batches = append(doc.Batches, batch)
	if len(doc.Batches) > s.batchIDsPerHour {
		doc.Batches = doc.Batches[len(doc.Batches)-s.batchIDsPerHour:]
	}
	return folded
}

// indexHours adds hours to the edge's index and removes, with their
// documents, the hours past retention.
func (s *Store) indexHours(ctx context.Context, edge string, add []int64, oldest int64) error {
	var held []int64
	if _, err := s.read(ctx, hoursKey(edge), &held); err != nil {
		return err
	}
	set := map[int64]bool{}
	var expired []int64
	for _, h := range held {
		if h < oldest {
			expired = append(expired, h)
			continue
		}
		set[h] = true
	}
	changed := len(expired) > 0
	for _, h := range add {
		if !set[h] {
			set[h] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	next := make([]int64, 0, len(set))
	for h := range set {
		next = append(next, h)
	}
	sort.Slice(next, func(i, j int) bool { return next[i] < next[j] })
	if err := s.write(ctx, hoursKey(edge), next); err != nil {
		return err
	}
	for _, h := range expired {
		if _, err := s.kv.Delete(ctx, hourKey(edge, h)); err != nil && !(s.notFound != nil && s.notFound(err)) {
			return err
		}
	}
	return nil
}

// indexEdge records the edge in the edge index. Only a new edge or a new
// latest hour writes it, so replicas rarely touch it at the same time; an
// edge lost to such a race is added again by its next batch.
func (s *Store) indexEdge(ctx context.Context, edge string, hour int64) error {
	doc := edgesDoc{}
	if _, err := s.read(ctx, "pc:edges", &doc); err != nil {
		return err
	}
	if doc.Edges == nil {
		doc.Edges = map[string]int64{}
	}
	if last, ok := doc.Edges[edge]; ok && last >= hour {
		return nil
	}
	if _, ok := doc.Edges[edge]; !ok && len(doc.Edges) >= MaxEdges {
		return errors.New("edge index is full")
	}
	doc.Edges[edge] = hour
	// Edges silent for longer than the retention hold no data any more
	oldest := HourOf(s.now().Add(-s.retention))
	for e, last := range doc.Edges {
		if last < oldest {
			delete(doc.Edges, e)
		}
	}
	return s.write(ctx, "pc:edges", doc)
}

func (s *Store) markPolicies(ctx context.Context, deltas []*Delta, oldest int64) error {
	type span struct{ first, last *Delta }
	seen := map[string]*span{}
	for _, d := range deltas {
		if d.PolicyHash == "" {
			continue
		}
		sp := seen[d.PolicyHash]
		if sp == nil {
			seen[d.PolicyHash] = &span{d, d}
			continue
		}
		if d.Hour < sp.first.Hour {
			sp.first = d
		}
		if d.Hour > sp.last.Hour {
			sp.last = d
		}
	}
	if len(seen) == 0 {
		return nil
	}
	doc := policiesDoc{}
	if _, err := s.read(ctx, policiesKey, &doc); err != nil {
		return err
	}
	if doc.Policies == nil {
		doc.Policies = map[string]*PolicyMarker{}
	}
	changed := false
	for hash, sp := range seen {
		m := doc.Policies[hash]
		if m == nil {
			doc.Policies[hash] = &PolicyMarker{Hash: hash, FirstSeen: sp.first.Hour, LastSeen: sp.last.Hour, Name: sp.first.PolicyName, Level: sp.first.Level}
			changed = true
			continue
		}
		if sp.first.Hour < m.FirstSeen {
			m.FirstSeen = sp.first.Hour
			changed = true
		}
		if sp.last.Hour > m.LastSeen {
			m.LastSeen = sp.last.Hour
			changed = true
		}
	}
	// Markers of policies not seen within the retention go; past the cap,
	// the least recently seen go first.
	for hash, m := range doc.Policies {
		if m.LastSeen < oldest {
			delete(doc.Policies, hash)
			changed = true
		}
	}
	if over := len(doc.Policies) - s.policyMarkers; over > 0 {
		list := make([]*PolicyMarker, 0, len(doc.Policies))
		for _, m := range doc.Policies {
			list = append(list, m)
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].LastSeen != list[j].LastSeen {
				return list[i].LastSeen < list[j].LastSeen
			}
			return list[i].Hash < list[j].Hash
		})
		for _, m := range list[:over] {
			delete(doc.Policies, m.Hash)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	return s.write(ctx, policiesKey, doc)
}

const policiesKey = "pc:policies"

type policiesDoc struct {
	Policies map[string]*PolicyMarker `json:"policies"`
}

// Query returns the rows for hours from..to (inclusive), merged across
// edges, and the markers of the policies they name.
func (s *Store) Query(ctx context.Context, from, to int64) ([]*Delta, []PolicyMarker, error) {
	edges := edgesDoc{}
	if _, err := s.read(ctx, "pc:edges", &edges); err != nil {
		return nil, nil, err
	}
	merged := map[string]*Delta{}
	for edge := range edges.Edges {
		var held []int64
		if _, err := s.read(ctx, hoursKey(edge), &held); err != nil {
			return nil, nil, err
		}
		for _, h := range held {
			if h < from || h > to {
				continue
			}
			doc := hourDoc{}
			if _, err := s.read(ctx, hourKey(edge, h), &doc); err != nil {
				return nil, nil, err
			}
			for row, d := range doc.Rows {
				if cur := merged[row]; cur != nil {
					cur.Merge(d)
				} else {
					cp := NewDelta(d.Key)
					cp.Merge(d)
					merged[row] = cp
				}
			}
		}
	}
	rows := make([]*Delta, 0, len(merged))
	hashes := map[string]bool{}
	for _, d := range merged {
		rows = append(rows, d)
		if d.PolicyHash != "" {
			hashes[d.PolicyHash] = true
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Row() < rows[j].Row() })

	pdoc := policiesDoc{}
	if _, err := s.read(ctx, policiesKey, &pdoc); err != nil {
		return nil, nil, err
	}
	markers := make([]PolicyMarker, 0, len(hashes))
	for h := range hashes {
		if m := pdoc.Policies[h]; m != nil {
			markers = append(markers, *m)
		}
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].FirstSeen < markers[j].FirstSeen })
	return rows, markers, nil
}
