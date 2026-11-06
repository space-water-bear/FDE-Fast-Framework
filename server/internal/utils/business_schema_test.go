package utils

import "testing"

func TestBusinessValidationRequiresPresenceButAcceptsFalseAndZero(t *testing.T) {
	schema := `{"fields":{"name":{"type":"string","required":true,"maxLength":3},"active":{"type":"boolean","required":true},"count":{"type":"integer","required":true,"minimum":0}}}`
	valid := map[string]any{"name": "维修", "active": false, "count": float64(0)}
	if err := ValidateBusinessValues(schema, valid); err != nil {
		t.Fatal(err)
	}
	for _, values := range []map[string]any{{"name": "维修", "count": float64(0)}, {"name": "超过三个字", "active": false, "count": float64(0)}, {"name": "维修", "active": false, "count": -1.0}, {"name": "维修", "active": "false", "count": float64(0)}} {
		if ValidateBusinessValues(schema, values) == nil {
			t.Fatalf("accepted invalid business request: %+v", values)
		}
	}
}
