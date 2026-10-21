package config

import (
	"strings"
	"testing"
)

func TestDropToolsConfig(t *testing.T) {
	a := mustParse(t, `{"policies": {"p": {"drop_tools": ["deploy_*", "legacy.search", "*"]}}}`)
	b := mustParse(t, `{"policies": {"p": {"drop_tools": ["*", "legacy.search", "deploy_*"]}}}`)
	none := mustParse(t, `{"policies": {"p": {}}}`)
	pa, _ := a.Policy("p")
	pb, _ := b.Policy("p")
	pn, _ := none.Policy("p")
	if pa.Hash() != pb.Hash() || pa.Hash() == pn.Hash() {
		t.Error("the drop list's order changed the hash, or the list did not")
	}
	for _, bad := range []string{`"a b"`, `"x*y"`, `""`, `"` + strings.Repeat("a", 66) + `"`} {
		if _, err := ParseJSON([]byte(`{"policies": {"p": {"drop_tools": [` + bad + `]}}}`)); err == nil || !strings.Contains(err.Error(), "drop_tools[0]") {
			t.Errorf("%s: %v", bad, err)
		}
	}
	many := strings.TrimSuffix(strings.Repeat(`"t",`, MaxDropTools+1), ",")
	if _, err := ParseJSON([]byte(`{"policies": {"p": {"drop_tools": [` + many + `]}}}`)); err == nil {
		t.Error("an over-long drop list was accepted")
	}
}
