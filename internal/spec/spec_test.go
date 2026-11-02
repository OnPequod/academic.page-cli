package spec

import (
	"encoding/json"
	"testing"
)

// make spec can fetch from a local app; the embedded document must still
// name the production API, so nothing built from it points at a dev host.
func TestEmbeddedSpecNamesProduction(t *testing.T) {
	var doc struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(Raw(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Servers) != 1 || doc.Servers[0].URL != "https://academic.page/api/v1" {
		t.Fatalf("servers = %+v; refresh with `make spec`, which resets it to production", doc.Servers)
	}
}
