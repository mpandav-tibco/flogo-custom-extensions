package model

// Finding is a single rule match result.
type Finding struct {
	RuleID         string   `json:"rule_id"`
	Severity       string   `json:"severity"`
	Category       string   `json:"category"`
	Title          string   `json:"title"`
	Location       string   `json:"location"`
	Message        string   `json:"message"`
	Recommendation string   `json:"recommendation"`
	Tags           []string `json:"tags"`
	RootCauses     []string `json:"root_causes,omitempty"`
	Fixes          []string `json:"fixes,omitempty"`
}

// Result aggregates all findings from an evaluation run.
type Result struct {
	Findings     []Finding              `json:"findings"`
	Positives    []Finding              `json:"positives"`
	ErrorCount   int                    `json:"error_count"`
	WarningCount int                    `json:"warning_count"`
	InfoCount    int                    `json:"info_count"`
	Markdown     string                 `json:"markdown"`
	Overview     map[string]interface{} `json:"overview"`
}

// FindingsAsInterface converts Findings to []interface{} for Flogo output compatibility.
func (r *Result) FindingsAsInterface() []interface{} {
	return toInterfaceSlice(r.Findings)
}

// PositivesAsInterface converts Positives to []interface{} for Flogo output compatibility.
func (r *Result) PositivesAsInterface() []interface{} {
	return toInterfaceSlice(r.Positives)
}

func toInterfaceSlice(findings []Finding) []interface{} {
	out := make([]interface{}, len(findings))
	for i, f := range findings {
		m := map[string]interface{}{
			"rule_id":        f.RuleID,
			"severity":       f.Severity,
			"category":       f.Category,
			"title":          f.Title,
			"location":       f.Location,
			"message":        f.Message,
			"recommendation": f.Recommendation,
			"tags":           stringsToInterface(f.Tags),
		}
		if len(f.RootCauses) > 0 {
			m["root_causes"] = stringsToInterface(f.RootCauses)
		}
		if len(f.Fixes) > 0 {
			m["fixes"] = stringsToInterface(f.Fixes)
		}
		out[i] = m
	}
	return out
}

// stringsToInterface converts []string to []interface{} for Flogo output compatibility.
// Flogo's data mapper expects homogeneous []interface{} arrays, not typed slices.
func stringsToInterface(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
