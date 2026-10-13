package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sequinstream/sequin/cli/context"
)

func TestPlanPreservesResourceValues(t *testing.T) {
	// Include numbers in both old and new resources, nested arrays, and a
	// numeric-looking string: preserving digits must not change JSON types.
	resource := `{"enabled":true,"filters":[{"comparison_value":[9007199254740993,9223372036854775807,-9223372036854775808],"operator":"in"}],"name":"9007199254740993","optional":null,"ratio":1.25}`
	response := fmt.Sprintf(`{"changes":[{"resource_type":"change_retention","action":"update","old":%s,"new":%s}],"actions":[{"description":"test action"}]}`, resource, resource)
	plan, err := planFromResponse(t, response)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || len(plan.Actions) != 1 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if plan.Changes[0].Action != "update" || plan.Changes[0].ResourceType != "change_retention" || plan.Actions[0].Description != "test action" {
		t.Fatalf("unexpected plan metadata: %#v", plan)
	}
	for label, value := range map[string]interface{}{"old": plan.Changes[0].Old, "new": plan.Changes[0].New} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(encoded) != resource {
			t.Errorf("%s resource changed during decoding:\nwant %s\ngot  %s", label, resource, encoded)
		}
	}
}

func TestPlanRejectsInvalidJSON(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"malformed", `{"changes":[}`},
		{"invalid number", `{"changes":[{"new":{"value":01}}]}`},
		{"trailing garbage", `{"changes":[]} garbage`},
		{"second object", `{"changes":[]} {"changes":[]}`},
		{"trailing null", `{"changes":[]} null`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := planFromResponse(t, tt.body)
			if plan != nil || err == nil || !strings.Contains(err.Error(), "failed to parse response") {
				t.Fatalf("expected a parse error, got plan=%#v err=%v", plan, err)
			}
		})
	}
}

func TestPlanAcceptsTrailingWhitespace(t *testing.T) {
	plan, err := planFromResponse(t, "{\"changes\":[],\"actions\":[]} \n\t\r")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 || len(plan.Actions) != 0 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func planFromResponse(t *testing.T, response string) (*PlanResponse, error) {
	t.Helper()
	yamlPath := filepath.Join(t.TempDir(), "sequin.yaml")
	if err := os.WriteFile(yamlPath, []byte("change_retentions: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/config/plan" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	return Plan(&context.Context{Hostname: strings.TrimPrefix(server.URL, "http://")}, yamlPath)
}
