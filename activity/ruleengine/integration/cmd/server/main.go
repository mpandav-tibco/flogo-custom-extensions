package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/mpandav-tibco/flogo-custom-extensions/activity/ruleengine/engine"
)

func main() {
	rulesPath := os.Getenv("RULES_PATH")
	if rulesPath == "" {
		rulesPath = "./rules"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"service": "rule-engine-service",
			"version": "1.0.0",
		})
	})

	mux.HandleFunc("/api/analyze", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Content        string   `json:"content"`
			FileName       string   `json:"fileName"`
			ParserOverride string   `json:"parserOverride"`
			DisabledRules  []string `json:"disabledRules"`
			Tags           []string `json:"tags"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "invalid request body: " + err.Error(),
			})
			return
		}

		result, err := engine.Evaluate(engine.Request{
			Content:        req.Content,
			FileName:       req.FileName,
			RulesPath:      rulesPath,
			ParserOverride: req.ParserOverride,
			DisabledRules:  req.DisabledRules,
			Tags:           req.Tags,
		})

		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":  false,
				"error":    err.Error(),
				"findings": []interface{}{},
				"positives": []interface{}{},
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"findings":     result.FindingsAsInterface(),
			"positives":    result.PositivesAsInterface(),
			"errorCount":   result.ErrorCount,
			"warningCount": result.WarningCount,
			"infoCount":    result.InfoCount,
			"markdown":     result.Markdown,
			"overview":     result.Overview,
			"error":        "",
		})
	})

	addr := ":7000"
	log.Printf("rule-engine-service listening on %s (rules: %s)", addr, rulesPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
