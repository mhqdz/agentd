package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaJSONValid(t *testing.T) {
	b, err := json.Marshal(SchemaJSON(Registry()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"required":null`) {
		t.Fatalf("required 不能是 null：%s", string(b)[:200])
	}
}
