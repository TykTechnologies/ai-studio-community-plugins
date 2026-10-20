package records

import (
	"context"
	"fmt"
	"testing"
)

func TestReasonsAreCountedAndBounded(t *testing.T) {
	agg := NewAggregator(nil)
	for _, reason := range []string{"body_too_large", "body_too_large", "app_scope_missing", "Not A Reason"} {
		r := req(base, 1, 1, hashA, OutcomeSkipped, 0, 0)
		r.Reason = reason
		agg.Add(r)
	}
	d := agg.Flush()[0].Deltas[0]
	if d.Reasons["body_too_large"] != 2 || d.Reasons["app_scope_missing"] != 1 || d.Reasons[OtherStrategy] != 1 {
		t.Errorf("reasons = %v", d.Reasons)
	}

	// Merging fresh names from many batches keeps the row bounded
	s, _ := newStore(base)
	ctx := context.Background()
	for i := 0; i < 40; i++ {
		x := NewDelta(d.Key)
		x.Requests = 1
		x.Reasons = map[string]int64{fmt.Sprintf("reason_%d", i): 1}
		if err := s.Apply(ctx, "", "e", &Batch{Version: BatchVersion, ID: newBatchID(), Deltas: []*Delta{x}}); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, _ := s.Query(ctx, d.Hour, d.Hour)
	var n int64
	for _, c := range rows[0].Reasons {
		n += c
	}
	if len(rows[0].Reasons) > MaxReasonsPerRow || n != 40 {
		t.Errorf("%d reason names holding %d requests, want at most %d names and 40", len(rows[0].Reasons), n, MaxReasonsPerRow)
	}

	// Edges may not send badly shaped reasons
	bad := &Batch{Version: BatchVersion, ID: newBatchID(), Deltas: []*Delta{NewDelta(d.Key)}}
	bad.Deltas[0].Reasons = map[string]int64{"<script>": 1}
	if bad.Validate() == nil {
		t.Error("a badly shaped reason was accepted")
	}
}
