package main

import "testing"

func TestDedupeTLSAutomationPoliciesForImportedHTTPSListener(t *testing.T) {
	internal := map[string]any{
		"subjects": []string{"app.example.test"},
		"issuers":  []any{map[string]any{"module": "internal"}},
	}
	acme := map[string]any{
		"subjects": []string{"edge.example.test"},
		"issuers":  []any{map[string]any{"module": "acme"}},
	}

	got := dedupeTLSAutomationPolicies([]any{internal, acme, internal})
	if len(got) != 2 {
		t.Fatalf("expected duplicate host policy to be removed, got %d policies", len(got))
	}
}
