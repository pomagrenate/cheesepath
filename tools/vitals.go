package tools

import (
	"context"
	"fmt"
	"encoding/json"
	"net/http"
	"io/ioutil"
)

// VitalsMonitorTool allows an agent to read real-time patient vitals from a medical membrane.
type VitalsMonitorTool struct {
	serverAddr string
}

func NewVitalsMonitorTool(serverAddr string) *VitalsMonitorTool {
	return &VitalsMonitorTool{serverAddr: serverAddr}
}

func (t *VitalsMonitorTool) Name() string {
	return "vitals_monitor"
}

func (t *VitalsMonitorTool) Description() string {
	return "Monitor real-time patient vital signs (heart rate, blood pressure) from the clinical telemetry membrane. Requires patient_id."
}

func (t *VitalsMonitorTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patient_id": map[string]any{
				"type": "string",
				"description": "ID of the patient to monitor (e.g. P-042)",
			},
		},
		"required": []string{"patient_id"},
	}
}

func (t *VitalsMonitorTool) Dangerous() bool {
	return false
}

func (t *VitalsMonitorTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	patientID, _ := args["patient_id"].(string)
	if patientID == "" {
		return "", fmt.Errorf("patient_id is required")
	}

	// In a real hospital, this would call a secure telemetry API.
	// Here we call the Po-Health server which proxies PomaiDB TimeSeries.
	url := fmt.Sprintf("%s/api/patients/%s/vitals?limit=5", t.serverAddr, patientID)
	
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to contact hospital server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("Patient %s vitals currently unavailable (status %d)", patientID, resp.StatusCode), nil
	}

	body, _ := ioutil.ReadAll(resp.Body)
	var vitals []map[string]any
	if err := json.Unmarshal(body, &vitals); err != nil {
		return "", fmt.Errorf("failed to parse vitals data: %w", err)
	}

	if len(vitals) == 0 {
		return fmt.Sprintf("No active telemetry found for patient %s", patientID), nil
	}

	// Industrial summary for LLM context
	latest := vitals[len(vitals)-1]
	summary := fmt.Sprintf("Latest Vitals for %s:\n- HR: %v bpm\n- BP: %v mmHg\n- Temp: %v°C\n- Timestamp: %v",
		patientID, latest["hr"], latest["bp"], latest["temp"], latest["ts"])
	
	return summary, nil
}
