package evaluator

import (
	"testing"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine/parser"
)

// docFromJSON builds a real JSON-parsed Document so the condition evaluator is
// exercised against the exact path-resolution behaviour used in production.
func docFromJSON(t *testing.T, jsonStr string) parser.Document {
	t.Helper()
	doc, err := (&parser.JSONParser{}).Parse(jsonStr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

func evalCond(t *testing.T, expr, jsonStr string) bool {
	t.Helper()
	doc := docFromJSON(t, jsonStr)
	scope, _ := doc.ResolveScope("")
	matched, err := EvalConditionDSL(expr, doc, scope[0])
	if err != nil {
		t.Fatalf("eval %q: %v", expr, err)
	}
	return matched
}

// These conditions are copied verbatim from the app's rule files
// (spec-validation.yaml, field-completeness.yaml, alias-normalization.yaml,
// nlu-extraction.yaml) — the test proves every DSL construct they use works.
func TestConditionDSL_AppRules(t *testing.T) {
	cases := []struct {
		name string
		expr string
		json string
		want bool
	}{
		// is null / == "" (required-field checks)
		{"required missing (absent)", `source_system is null OR source_system == ""`, `{}`, true},
		{"required missing (empty)", `source_system is null OR source_system == ""`, `{"source_system":""}`, true},
		{"required present", `source_system is null OR source_system == ""`, `{"source_system":"s4hana"}`, false},

		// NOT in [list] — interface_type membership
		{"interface invalid", `interface_type NOT in ["pub", "sub", "service"]`, `{"interface_type":"badtype"}`, true},
		{"interface valid pub", `interface_type NOT in ["pub", "sub", "service"]`, `{"interface_type":"pub"}`, false},
		{"interface valid service", `interface_type NOT in ["pub", "sub", "service"]`, `{"interface_type":"service"}`, false},

		// email format (does not match) — AND chain + null guard
		{"email valid", `email is not null AND email != "NA" AND email does not match /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/`, `{"email":"dev@adidas.com"}`, false},
		{"email invalid", `email is not null AND email != "NA" AND email does not match /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/`, `{"email":"not-an-email"}`, true},
		{"email NA skipped", `email is not null AND email != "NA" AND email does not match /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/`, `{"email":"NA"}`, false},
		{"email absent skipped", `email is not null AND email != "NA" AND email does not match /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/`, `{}`, false},

		// email domain NOT in [...]
		{"domain allowed", `email is not null AND email != "NA" AND email domain NOT in ["adidas.com", "tibco.com"]`, `{"email":"dev@adidas.com"}`, false},
		{"domain blocked", `email is not null AND email != "NA" AND email domain NOT in ["adidas.com", "tibco.com"]`, `{"email":"dev@gmail.com"}`, true},

		// length() > number
		{"repo too long", `repo_name is not null AND length(repo_name) > 50`, `{"repo_name":"` + str(60, 'a') + `"}`, true},
		{"repo short ok", `repo_name is not null AND length(repo_name) > 50`, `{"repo_name":"short-repo"}`, false},
		{"repo absent ok", `repo_name is not null AND length(repo_name) > 50`, `{}`, false},

		// repo name chars (does not match)
		{"repo bad chars", `repo_name is not null AND repo_name does not match /^[a-z0-9-]+$/`, `{"repo_name":"Bad_Repo!"}`, true},
		{"repo good chars", `repo_name is not null AND repo_name does not match /^[a-z0-9-]+$/`, `{"repo_name":"s4hana-kafka-pub"}`, false},

		// env not matches (warning)
		{"env valid", `env is not null AND env not matches /^(DEV|SIT|UAT|PROD)(,(DEV|SIT|UAT|PROD))*$/`, `{"env":"DEV"}`, false},
		{"env valid combo", `env is not null AND env not matches /^(DEV|SIT|UAT|PROD)(,(DEV|SIT|UAT|PROD))*$/`, `{"env":"DEV,PROD"}`, false},
		{"env invalid", `env is not null AND env not matches /^(DEV|SIT|UAT|PROD)(,(DEV|SIT|UAT|PROD))*$/`, `{"env":"STAGING"}`, true},

		// source == target field-to-field comparison
		{"same systems", `source_system == target_system AND source_system != "rest"`, `{"source_system":"kafka","target_system":"kafka"}`, true},
		{"same but rest", `source_system == target_system AND source_system != "rest"`, `{"source_system":"rest","target_system":"rest"}`, false},
		{"different systems", `source_system == target_system AND source_system != "rest"`, `{"source_system":"s4hana","target_system":"kafka"}`, false},

		// in [list] — alias normalization
		{"alias pub", `interface_type in ["publish", "publisher", "produce", "PUBLISH"]`, `{"interface_type":"publish"}`, true},
		{"alias case-sensitive", `interface_type in ["publish", "PUBLISH"]`, `{"interface_type":"PUBLISH"}`, true},
		{"alias miss", `interface_type in ["publish", "publisher"]`, `{"interface_type":"pub"}`, false},

		// OR across two fields
		{"sf source", `source_system in ["sf", "sfdc"] OR target_system in ["sf", "sfdc"]`, `{"source_system":"sfdc","target_system":"kafka"}`, true},
		{"sf target", `source_system in ["sf", "sfdc"] OR target_system in ["sf", "sfdc"]`, `{"source_system":"s4hana","target_system":"sf"}`, true},
		{"sf neither", `source_system in ["sf", "sfdc"] OR target_system in ["sf", "sfdc"]`, `{"source_system":"s4hana","target_system":"kafka"}`, false},

		// message matches /regex/ — NLU
		{"ikd id present", `message matches /(?i)\bIKD-[0-9]{1,4}\b/`, `{"message":"use interface IKD-042 please"}`, true},
		{"ikd id absent", `message matches /(?i)\bIKD-[0-9]{1,4}\b/`, `{"message":"publish article from s4hana"}`, false},
		{"pub verb", `message matches /(?i)\b(publish|publisher|producer|outbound)\b/`, `{"message":"I want to publish events"}`, true},
		{"sub verb", `message matches /(?i)\b(subscribe|consumer|inbound|listen)\b/`, `{"message":"subscribe to the topic"}`, true},

		// any field contains [list] — CDM detection across nested fields
		{"cdm in message", `any field contains ["cdm", "canonical data model", "common data model"]`, `{"message":"please enable CDM transform","current_spec":{}}`, true},
		{"cdm in nested", `any field contains ["cdm", "canonical model"]`, `{"current_spec":{"note":"uses canonical model"}}`, true},
		{"cdm absent", `any field contains ["cdm", "canonical model"]`, `{"message":"publish article"}`, false},

		// multi-line AND chain (all_required_fields_complete)
		{"all complete", `source_system is not null AND source_system != "" AND target_system is not null AND target_system != "" AND interface_type in ["pub", "sub", "service"] AND template_type is not null AND template_type != ""`,
			`{"source_system":"s4hana","target_system":"kafka","interface_type":"pub","template_type":"X"}`, true},
		{"all complete missing one", `source_system is not null AND source_system != "" AND target_system is not null AND target_system != "" AND interface_type in ["pub", "sub", "service"] AND template_type is not null AND template_type != ""`,
			`{"source_system":"s4hana","target_system":"kafka","interface_type":"pub"}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalCond(t, tc.expr, tc.json)
			if got != tc.want {
				t.Fatalf("condition %q on %s = %v, want %v", tc.expr, tc.json, got, tc.want)
			}
		})
	}
}

func TestConditionDSL_Errors(t *testing.T) {
	bad := []string{
		`source_system ==`,      // missing right operand
		`source_system is`,      // incomplete is-null
		`interface_type in`,     // missing list
		`source_system matches`, // missing regex
		`source_system === "x"`, // bad operator
	}
	doc := docFromJSON(t, `{"source_system":"x"}`)
	scope, _ := doc.ResolveScope("")
	for _, expr := range bad {
		if _, err := EvalConditionDSL(expr, doc, scope[0]); err == nil {
			t.Errorf("expected parse error for %q, got nil", expr)
		}
	}
}

// str returns a string of n copies of c — helper for the repo-length test.
func str(n int, c rune) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
