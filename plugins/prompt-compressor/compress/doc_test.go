package compress

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// A body the caller already parsed (Input.Doc, in any format) compresses to
// the same bytes and report as one the compressor parses itself (R1), and a
// Doc of other bytes is not used.
func TestCompressWithCallersDoc(t *testing.T) {
	for _, body := range [][]byte{agentBody(2, 4096), agentBody(12, 163840)} {
		for _, level := range []int{1, 2, 3} {
			p := config.DefaultPolicy()
			p.Level, p.MinUnitTokens, p.ProtectRecentTurns = level, 0, 0
			want, wantRep := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p})
			doc, err := segments.Parse(segments.FormatAnthropic, body)
			if err != nil {
				t.Fatal(err)
			}
			got, gotRep := New(Options{}).Compress(Input{Format: "openai", Body: body, Doc: doc, Policy: p})
			if !bytes.Equal(got, want) || !reflect.DeepEqual(gotRep, wantRep) || wantRep.Outcome != OutcomeCompressed {
				t.Fatalf("L%d: the caller's Doc changed the result (%s / %s)", level, gotRep.Outcome, wantRep.Outcome)
			}
			other := append([]byte(nil), body...)
			got, gotRep = New(Options{}).Compress(Input{Format: "openai", Body: other, Doc: doc, Policy: p})
			if !bytes.Equal(got, want) || !reflect.DeepEqual(gotRep, wantRep) {
				t.Fatalf("L%d: a Doc of a copy changed the result", level)
			}
		}
	}
}
