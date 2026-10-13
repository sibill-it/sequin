package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sequinstream/sequin/cli/config"
	"github.com/sequinstream/sequin/cli/context"
)

func TestPlanNumericFilterDiff(t *testing.T) {
	tests := []struct {
		name    string
		old     string
		new     string
		changed bool
	}{
		{"adjacent integers above 2^53", "9007199254740992", "9007199254740993", true},
		{"adjacent negative integers below -2^53", "-9007199254740992", "-9007199254740993", true},
		{"adjacent integers at bigint limit", "9223372036854775806", "9223372036854775807", true},
		{"ordinary integers", "10", "11", true},
		{"ordinary decimals", "1.25", "1.5", true},
		{"number changed to string", "9007199254740993", `"9007199254740993"`, true},
		{"string changed to number", `"9007199254740993"`, "9007199254740993", true},
		{"unchanged large integer", "9007199254740993", "9007199254740993", false},
		{"unchanged decimal", "1.25", "1.25", false},
		{"unchanged numeric string", `"9007199254740993"`, `"9007199254740993"`, false},
	}

	yamlPath := filepath.Join(t.TempDir(), "sequin.yaml")
	if err := os.WriteFile(yamlPath, []byte("change_retentions: []\n"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Change-retention filters using "in" retain their integer list values
			// on the server, including PostgreSQL bigint values.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/config/plan" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"changes":[{"resource_type":"change_retention","action":"update",
					"old":{"name":"retention","filters":[{"column_name":"id","operator":"in","comparison_value":[%s]}]},
					"new":{"name":"retention","filters":[{"column_name":"id","operator":"in","comparison_value":[%s]}]}}],"actions":[]}`,
					tt.old, tt.new)
			}))
			defer server.Close()

			plan, err := config.Plan(&context.Context{Hostname: strings.TrimPrefix(server.URL, "http://")}, yamlPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Changes) != 1 {
				t.Fatalf("expected one resource, got %d", len(plan.Changes))
			}

			cmd := &ConfigCommands{}
			diff := capturePlanOutput(t, cmd, plan)
			if !tt.changed {
				if cmd.changes != 0 || !strings.Contains(diff, "No changes detected") {
					t.Fatalf("unchanged filter produced %d changes: %s", cmd.changes, diff)
				}
				return
			}
			if cmd.changes != 1 || !strings.Contains(diff, "Plan: 0 to add, 1 to change, 0 to destroy.") {
				t.Fatalf("expected one update available to apply, got %d changes: %s", cmd.changes, diff)
			}
			if !strings.Contains(diff, strings.Trim(tt.old, `"`)) || !strings.Contains(diff, strings.Trim(tt.new, `"`)) || !strings.Contains(diff, "→") {
				t.Fatalf("plan must show both exact filter values and the change: %s", diff)
			}
		})
	}
}

func TestPrettyDiffJSONNumberCreatesAndDeletes(t *testing.T) {
	resource := map[string]interface{}{"value": json.Number("9223372036854775807")}
	for _, tt := range []struct {
		name string
		old  map[string]interface{}
		new  map[string]interface{}
	}{
		{"create", nil, resource},
		{"delete", resource, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if output := PrettyDiff(tt.old, tt.new); !strings.Contains(output, "9223372036854775807") {
				t.Fatalf("expected the exact value in the diff, got %s", output)
			}
		})
	}
}

func capturePlanOutput(t *testing.T, cmd *ConfigCommands, plan *config.PlanResponse) string {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "plan-output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previous := os.Stdout
	os.Stdout = output
	defer func() { os.Stdout = previous }()
	if err := cmd.displayPlan(plan); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
