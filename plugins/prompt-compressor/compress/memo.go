package compress

import (
	"container/list"
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

// memoKey identifies one leaf's compression: its text and the policy hash
// (which folds in the strategy version), each length-prefixed so no two
// inputs share a key.
type memoKey [sha256.Size]byte

func newMemoKey(policyHash, text string) memoKey {
	h := sha256.New()
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(policyHash)))
	h.Write(n[:])
	h.Write([]byte(policyHash))
	binary.BigEndian.PutUint64(n[:], uint64(len(text)))
	h.Write(n[:])
	h.Write([]byte(text))
	var k memoKey
	h.Sum(k[:0])
	return k
}

// memoValue is a compressed leaf and the strategies that produced it. An
// unchanged leaf is remembered too (Same), so it is not tried again.
type memoValue struct {
	Text  string
	Steps []step
	Same  bool
	// Tokens is a token count, in the token-count memo.
	Tokens int
}

// memo is an LRU of compressed leaves bounded by the bytes it holds, so a
// burst of large tool outputs cannot grow it without limit. The output is
// the same with or without it.
type memo struct {
	mu       sync.Mutex
	maxBytes int
	bytes    int
	order    *list.List
	entries  map[memoKey]*list.Element
}

type memoEntry struct {
	key   memoKey
	value memoValue
	size  int
}

func newMemo(maxBytes int) *memo {
	return &memo{maxBytes: maxBytes, order: list.New(), entries: map[memoKey]*list.Element{}}
}

func (m *memo) get(k memoKey) (memoValue, bool) {
	if m == nil || m.maxBytes <= 0 {
		return memoValue{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	el, ok := m.entries[k]
	if !ok {
		return memoValue{}, false
	}
	m.order.MoveToFront(el)
	return el.Value.(*memoEntry).value, true
}

func (m *memo) put(k memoKey, v memoValue) {
	if m == nil || m.maxBytes <= 0 {
		return
	}
	size := len(k) + len(v.Text) + 64
	if size > m.maxBytes/4 {
		return // one entry may not take over the cache
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if el, ok := m.entries[k]; ok {
		m.order.MoveToFront(el)
		return
	}
	m.entries[k] = m.order.PushFront(&memoEntry{key: k, value: v, size: size})
	m.bytes += size
	for m.bytes > m.maxBytes {
		last := m.order.Back()
		e := last.Value.(*memoEntry)
		m.order.Remove(last)
		delete(m.entries, e.key)
		m.bytes -= e.size
	}
}

func (m *memo) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}
