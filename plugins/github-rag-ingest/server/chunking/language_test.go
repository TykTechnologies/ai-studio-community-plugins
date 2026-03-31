package chunking

import (
	"testing"
)

func TestIsBinaryFile(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{
			name:    "pure ASCII text",
			content: []byte("Hello, world!\nThis is a test file.\n"),
			want:    false,
		},
		{
			name:    "valid multi-byte UTF-8",
			content: []byte("café ☕ 日本語 emoji 🎉"),
			want:    false,
		},
		{
			name:    "contains null bytes (binary)",
			content: []byte("hello\x00world\x00test"),
			want:    true,
		},
		{
			name:    "random binary data (high invalid UTF-8 ratio)",
			content: makeRandomBinaryData(1024),
			want:    true,
		},
		{
			name:    "Latin-1 text with a few accented chars (low ratio, not binary)",
			content: []byte("This is a document about caf\xe9s in Paris.\nThe r\xe9sum\xe9 was well written.\nMost of the text is plain ASCII.\n"),
			want:    false,
		},
		{
			name:    "empty content",
			content: []byte{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBinaryFile(tt.content)
			if got != tt.want {
				t.Errorf("IsBinaryFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

// makeRandomBinaryData creates a byte slice with a high ratio of non-UTF-8 bytes
func makeRandomBinaryData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		// Generate bytes 0x80-0xFF which are invalid single-byte UTF-8
		data[i] = byte(0x80 + (i % 128))
	}
	return data
}
