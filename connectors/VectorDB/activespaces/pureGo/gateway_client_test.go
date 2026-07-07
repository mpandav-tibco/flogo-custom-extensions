package vectordb

import "testing"

func TestTranslateFilters_Eq(t *testing.T) {
	f := translateFilters(map[string]interface{}{"category": "news"})
	if f == nil || f.Op != "eq" || f.Field != "category" || f.Value != "news" {
		t.Fatalf("unexpected filter: %+v", f)
	}
}

func TestTranslateFilters_Operators(t *testing.T) {
	f := translateFilters(map[string]interface{}{"year": map[string]interface{}{"$gte": 2020}})
	if f == nil || f.Op != "gte" || f.Field != "year" || f.Value != 2020 {
		t.Fatalf("unexpected filter: %+v", f)
	}
}

func TestTranslateFilters_In(t *testing.T) {
	f := translateFilters(map[string]interface{}{"tags": []interface{}{"a", "b"}})
	if f == nil || f.Op != "in" || f.Field != "tags" {
		t.Fatalf("unexpected filter: %+v", f)
	}
}

func TestTranslateFilters_Multiple(t *testing.T) {
	f := translateFilters(map[string]interface{}{"a": "1", "b": "2"})
	if f == nil || f.Op != "and" || len(f.Filters) != 2 {
		t.Fatalf("expected AND of 2 conditions, got: %+v", f)
	}
}

func TestTranslateFilters_Empty(t *testing.T) {
	if translateFilters(nil) != nil {
		t.Fatal("expected nil for empty filter")
	}
}

func TestToSimilarity(t *testing.T) {
	cases := map[string]string{
		"cosine": "cosine", "": "cosine", "COSINE": "cosine",
		"dot": "dot", "dotproduct": "dot",
		"euclidean": "l2", "l2": "l2",
	}
	for in, want := range cases {
		if got := toSimilarity(in); got != want {
			t.Errorf("toSimilarity(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMapFilterOp(t *testing.T) {
	cases := map[string]string{
		"$eq": "eq", "$ne": "ne", "$gt": "gt", "$gte": "gte",
		"$lt": "lt", "$lte": "lte", "$in": "in", "$like": "like",
		"unknown": "eq",
	}
	for in, want := range cases {
		if got := mapFilterOp(in); got != want {
			t.Errorf("mapFilterOp(%q)=%q want %q", in, got, want)
		}
	}
}
