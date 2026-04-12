package tools

// clinical.go — Po-Health-specific CrabTools for the clinical agent.
//
// These tools call the Po-Health FastAPI server (default http://localhost:8000)
// and implement the CrabTool interface defined in interface.go.
//
// No existing files in this package were modified.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// newClinicalHTTPClient returns a shared http.Client for all clinical tools.
func newClinicalHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// doGET is a helper that performs a GET and returns the response body as string.
func doGET(addr string) (string, error) {
	resp, err := newClinicalHTTPClient().Get(addr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

// doPOSTJSON posts a JSON payload and returns the response body as string.
func doPOSTJSON(addr string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	resp, err := newClinicalHTTPClient().Post(addr, "application/json", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

// ---------------------------------------------------------------------------
// SearchGuidelinesTool
// ---------------------------------------------------------------------------

// SearchGuidelinesTool queries /api/guidelines/search for clinical literature.
type SearchGuidelinesTool struct{ addr string }

// NewSearchGuidelinesTool creates a SearchGuidelinesTool pointing at poHealthAddr.
func NewSearchGuidelinesTool(poHealthAddr string) *SearchGuidelinesTool {
	return &SearchGuidelinesTool{addr: poHealthAddr}
}

func (t *SearchGuidelinesTool) Name() string { return "search_clinical_guidelines" }
func (t *SearchGuidelinesTool) Description() string {
	return "Search clinical guidelines and ingested medical literature for a given query. " +
		"Returns relevant excerpts with source titles and relevance scores."
}
func (t *SearchGuidelinesTool) Dangerous() bool { return false }
func (t *SearchGuidelinesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Clinical question or topic to search (e.g. 'warfarin bleeding risk')",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default 5)",
			},
		},
		"required": []string{"query"},
	}
}
func (t *SearchGuidelinesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	q, _ := args["query"].(string)
	if q == "" {
		return "", fmt.Errorf("query is required")
	}
	topK := 5
	if v, ok := args["top_k"].(float64); ok && v > 0 {
		topK = int(v)
	}
	u := fmt.Sprintf("%s/api/guidelines/search?q=%s&top_k=%d",
		t.addr, url.QueryEscape(q), topK)
	return doGET(u)
}

// ---------------------------------------------------------------------------
// CheckDDITool
// ---------------------------------------------------------------------------

// CheckDDITool calls /api/check-interactions with a list of numeric drug IDs.
type CheckDDITool struct{ addr string }

// NewCheckDDITool creates a CheckDDITool pointing at poHealthAddr.
func NewCheckDDITool(poHealthAddr string) *CheckDDITool {
	return &CheckDDITool{addr: poHealthAddr}
}

func (t *CheckDDITool) Name() string { return "check_drug_interactions" }
func (t *CheckDDITool) Description() string {
	return "Check for Drug-Drug Interactions (DDI) between multiple drugs using the " +
		"Po-Health interaction graph. Pass a list of numeric drug IDs. " +
		"Returns severity, mechanism, and supporting literature for each interaction."
}
func (t *CheckDDITool) Dangerous() bool { return false }
func (t *CheckDDITool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"drug_ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "integer"},
				"description": "List of numeric Po-Health drug IDs to check for interactions",
			},
		},
		"required": []string{"drug_ids"},
	}
}
func (t *CheckDDITool) Execute(ctx context.Context, args map[string]any) (string, error) {
	ids, ok := args["drug_ids"]
	if !ok {
		return "", fmt.Errorf("drug_ids is required")
	}
	return doPOSTJSON(t.addr+"/api/check-interactions", map[string]any{"drug_ids": ids})
}

// ---------------------------------------------------------------------------
// GetPatientMedsTool
// ---------------------------------------------------------------------------

// GetPatientMedsTool retrieves a patient's current medication list.
type GetPatientMedsTool struct{ addr string }

// NewGetPatientMedsTool creates a GetPatientMedsTool pointing at poHealthAddr.
func NewGetPatientMedsTool(poHealthAddr string) *GetPatientMedsTool {
	return &GetPatientMedsTool{addr: poHealthAddr}
}

func (t *GetPatientMedsTool) Name() string { return "get_patient_medications" }
func (t *GetPatientMedsTool) Description() string {
	return "Retrieve the current medication list for a patient by their patient_id. " +
		"Returns drug names, IDs, doses, and the date each medication was added."
}
func (t *GetPatientMedsTool) Dangerous() bool { return false }
func (t *GetPatientMedsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patient_id": map[string]any{
				"type":        "string",
				"description": "Patient identifier (e.g. 'P001')",
			},
		},
		"required": []string{"patient_id"},
	}
}
func (t *GetPatientMedsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	pid, _ := args["patient_id"].(string)
	if pid == "" {
		return "", fmt.Errorf("patient_id is required")
	}
	return doGET(fmt.Sprintf("%s/api/patients/%s/medications",
		t.addr, url.PathEscape(pid)))
}

// ---------------------------------------------------------------------------
// SearchDrugsTool
// ---------------------------------------------------------------------------

// SearchDrugsTool performs a semantic search over the FDA drug database.
type SearchDrugsTool struct{ addr string }

// NewSearchDrugsTool creates a SearchDrugsTool pointing at poHealthAddr.
func NewSearchDrugsTool(poHealthAddr string) *SearchDrugsTool {
	return &SearchDrugsTool{addr: poHealthAddr}
}

func (t *SearchDrugsTool) Name() string { return "search_drugs" }
func (t *SearchDrugsTool) Description() string {
	return "Semantic search over the FDA drug label database. " +
		"Returns drugs with similarity scores, indications, ingredients, and risk flags."
}
func (t *SearchDrugsTool) Dangerous() bool { return false }
func (t *SearchDrugsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Clinical query or drug name",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results (default 5)",
			},
		},
		"required": []string{"query"},
	}
}
func (t *SearchDrugsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	topK := 5
	if v, ok := args["top_k"].(float64); ok && v > 0 {
		topK = int(v)
	}
	return doPOSTJSON(t.addr+"/api/search",
		map[string]any{"query": query, "top_k": topK})
}

// ---------------------------------------------------------------------------
// GetPatientAlertsTool
// ---------------------------------------------------------------------------

// GetPatientAlertsTool retrieves recent clinical alerts for a patient.
type GetPatientAlertsTool struct{ addr string }

// NewGetPatientAlertsTool creates a GetPatientAlertsTool pointing at poHealthAddr.
func NewGetPatientAlertsTool(poHealthAddr string) *GetPatientAlertsTool {
	return &GetPatientAlertsTool{addr: poHealthAddr}
}

func (t *GetPatientAlertsTool) Name() string { return "get_patient_alerts" }
func (t *GetPatientAlertsTool) Description() string {
	return "Retrieve recent clinical alerts (vital-sign threshold breaches) for a patient. " +
		"Each alert includes the vital name, value, severity, suspected medications, " +
		"and RAG evidence from the drug database."
}
func (t *GetPatientAlertsTool) Dangerous() bool { return false }
func (t *GetPatientAlertsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patient_id": map[string]any{
				"type":        "string",
				"description": "Patient identifier",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of alerts to return (default 10)",
			},
		},
		"required": []string{"patient_id"},
	}
}
func (t *GetPatientAlertsTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	pid, _ := args["patient_id"].(string)
	if pid == "" {
		return "", fmt.Errorf("patient_id is required")
	}
	limit := 10
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	return doGET(fmt.Sprintf("%s/api/patients/%s/alerts?limit=%d",
		t.addr, url.PathEscape(pid), limit))
}

// ---------------------------------------------------------------------------
// NewClinicalRegistry
// ---------------------------------------------------------------------------

// NewClinicalRegistry creates a Registry pre-loaded with all Po-Health clinical
// tools. It does NOT include file-system, shell, or model-switching tools so
// the agent stays scoped to read-only clinical queries.
func NewClinicalRegistry(poHealthAddr string) *Registry {
	r := NewRegistry()
	r.Register(NewSearchGuidelinesTool(poHealthAddr))
	r.Register(NewCheckDDITool(poHealthAddr))
	r.Register(NewGetPatientMedsTool(poHealthAddr))
	r.Register(NewSearchDrugsTool(poHealthAddr))
	r.Register(NewGetPatientAlertsTool(poHealthAddr))
	return r
}
