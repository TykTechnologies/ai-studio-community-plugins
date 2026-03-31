package chunking

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGetOverlapDoesNotSplitUTF8(t *testing.T) {
	tests := []struct {
		name string
		text string
		size int
	}{
		{
			name: "overlap boundary lands inside checkmark emoji",
			// ✅ is \xe2\x9c\x85 (3 bytes). If size cuts after \xe2, we get broken UTF-8
			text: "some text before ✅ **Response Filters**",
			size: 30,
		},
		{
			name: "overlap boundary lands inside em dash",
			// — is \xe2\x80\x94 (3 bytes)
			text: "Security — REQUIRED: replace with your key",
			size: 35,
		},
		{
			name: "overlap boundary lands inside CJK character",
			text: "test 日本語 content here for chunking",
			size: 20,
		},
		{
			name: "all ASCII (no issue)",
			text: "just plain ascii text here nothing special",
			size: 20,
		},
		{
			name: "text shorter than size",
			text: "short ✅",
			size: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getOverlap(tt.text, tt.size)
			if !utf8.ValidString(result) {
				t.Errorf("getOverlap produced invalid UTF-8: %q", result)
			}
		})
	}
}

func TestSimpleChunkerPreservesUTF8(t *testing.T) {
	// Reproduce the exact bug: filters.mdx content with ✅ near chunk boundary
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "- ✅ **Request Filters**: Inspect and modify incoming user messages")
	}
	content := []byte(strings.Join(lines, "\n"))

	chunker := NewSimpleChunker(2000, 200)
	chunks, err := chunker.Chunk(content, "test.md", "markdown")
	if err != nil {
		t.Fatalf("chunking failed: %v", err)
	}

	for i, chunk := range chunks {
		if !utf8.ValidString(chunk.Content) {
			t.Errorf("chunk %d has invalid UTF-8: preview=%q", i,
				chunk.Content[:min(80, len(chunk.Content))])
		}
	}
	t.Logf("produced %d chunks, all valid UTF-8", len(chunks))
}
