package reporting

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/usestrix/strix-go/pkg/tools"
)

type VulnerabilityReport struct {
	ReportID           string    `json:"report_id"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	Impact             string    `json:"impact"`
	Target             string    `json:"target"`
	TechnicalAnalysis  string    `json:"technical_analysis"`
	PocDescription     string    `json:"poc_description"`
	PocScriptCode      string    `json:"poc_script_code"`
	RemediationSteps   string    `json:"remediation_steps"`
	Endpoint           string    `json:"endpoint,omitempty"`
	Method             string    `json:"method,omitempty"`
	CVE                string    `json:"cve,omitempty"`
	CWE                string    `json:"cwe,omitempty"`
	CVSSScore          float64   `json:"cvss_score"`
	CVSSSeverity       string    `json:"cvss_severity"`
	CVSSVector         string    `json:"cvss_vector,omitempty"`
	CreatedAt          string    `json:"created_at"`
}

type ReportEvent struct {
	Timestamp string                 `json:"timestamp"`
	Op        string                 `json:"op"`
	ReportID  string                 `json:"report_id"`
	Report    *VulnerabilityReport   `json:"report,omitempty"`
}

var (
	RunDir           string
	loadedRunDir     string
	reportsStorage   = make(map[string]*VulnerabilityReport)
	reportsLock      sync.RWMutex
)

func RegisterReportingTools() {
	tools.Register("create_vulnerability_report", false, CreateVulnerabilityReport)
}

func getReportsJSONLPath() string {
	if RunDir == "" {
		return ""
	}
	reportsDir := filepath.Join(RunDir, "reports")
	_ = os.MkdirAll(reportsDir, 0755)
	return filepath.Join(reportsDir, "reports.jsonl")
}

func appendReportEvent(op string, reportID string, report *VulnerabilityReport) {
	path := getReportsJSONLPath()
	if path == "" {
		return
	}

	event := ReportEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Op:        op,
		ReportID:  reportID,
		Report:    report,
	}

	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("Failed to marshal report event", slog.Any("error", err))
		return
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		slog.Error("Failed to open reports JSONL file", slog.String("path", path), slog.Any("error", err))
		return
	}
	defer f.Close()

	_, _ = f.WriteString(string(data) + "\n")
}

func generateReportID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("report_%s", hex.EncodeToString(b))
}

// CVSS3 Severity ratings
func getSeverityFromScore(score float64) string {
	switch {
	case score == 0:
		return "none"
	case score > 0 && score < 3.9:
		return "low"
	case score >= 3.9 && score < 6.9:
		return "medium"
	case score >= 6.9 && score < 8.9:
		return "high"
	case score >= 8.9:
		return "critical"
	default:
		return "high"
	}
}

// CVSS3 Base Score Calculation
// Formula based on CVSS v3.1 specification
func calculateCVSS3BaseScore(
	av, ac, pr, ui, s, c, i, a string,
) (float64, string) {
	// Map string values to numeric values
	avMap := map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2}
	acMap := map[string]float64{"L": 0.77, "H": 0.44}
	prUnchangedMap := map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	prChangedMap := map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}
	uiMap := map[string]float64{"N": 0.85, "R": 0.62}
	ciaMap := map[string]float64{"N": 0.0, "L": 0.22, "H": 0.56}

	avVal, avOk := avMap[strings.ToUpper(av)]
	acVal, acOk := acMap[strings.ToUpper(ac)]
	uiVal, uiOk := uiMap[strings.ToUpper(ui)]
	cVal, cOk := ciaMap[strings.ToUpper(c)]
	iVal, iOk := ciaMap[strings.ToUpper(i)]
	aVal, aOk := ciaMap[strings.ToUpper(a)]
	sVal := strings.ToUpper(s)

	// PR depends on Scope
	var prVal float64
	var prOk bool
	if sVal == "U" {
		prVal, prOk = prUnchangedMap[strings.ToUpper(pr)]
	} else if sVal == "C" {
		prVal, prOk = prChangedMap[strings.ToUpper(pr)]
	} else {
		prOk = false
	}

	// Validate all values
	if !avOk || !acOk || !prOk || !uiOk || !cOk || !iOk || !aOk {
		// Invalid CVSS values, return fallback
		return 7.5, "high"
	}

	// Calculate Impact
	cia := 1 - ((1 - cVal) * (1 - iVal) * (1 - aVal))
	var impact float64

	if sVal == "U" {
		impact = 6.42 * cia
	} else if sVal == "C" {
		impact = 7.52*(cia-0.029) - 3.25*math.Pow(cia-0.02, 15)
	} else {
		return 7.5, "high"
	}

	// Calculate Exploitability
	exploitability := 8.22 * avVal * acVal * prVal * uiVal

	// Calculate Base Score
	var baseScore float64
	if impact <= 0 {
		baseScore = 0
	} else if sVal == "U" {
		baseScore = math.Min(impact+exploitability, 10)
	} else {
		baseScore = math.Min(1.08*(impact+exploitability), 10)
	}

	// Round up to 1 decimal place
	baseScore = math.Ceil(baseScore*10) / 10

	severity := getSeverityFromScore(baseScore)
	return baseScore, severity
}

// Extract CVSS values from XML string like <attack_vector>N</attack_vector>
func extractCVSSValues(xmlStr string) (av, ac, pr, ui, s, c, i, a string, valid bool) {
	fields := map[string]*string{
		"attack_vector":       &av,
		"attack_complexity":   &ac,
		"privileges_required": &pr,
		"user_interaction":    &ui,
		"scope":                &s,
		"confidentiality":     &c,
		"integrity":           &i,
		"availability":        &a,
	}

	for field, ptr := range fields {
		pattern := fmt.Sprintf(`<%s>([^<]+)</%s>`, field, field)
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(xmlStr)
		if len(matches) > 1 {
			*ptr = strings.TrimSpace(matches[1])
		}
	}

	// All 8 fields must be present and non-empty
	valid = av != "" && ac != "" && pr != "" && ui != "" && s != "" && c != "" && i != "" && a != ""
	return
}

func CreateVulnerabilityReport(args map[string]interface{}) (interface{}, error) {
	reportsLock.Lock()
	defer reportsLock.Unlock()

	// Extract and validate required fields
	title, _ := args["title"].(string)
	description, _ := args["description"].(string)
	impact, _ := args["impact"].(string)
	target, _ := args["target"].(string)
	technicalAnalysis, _ := args["technical_analysis"].(string)
	pocDescription, _ := args["poc_description"].(string)
	pocScriptCode, _ := args["poc_script_code"].(string)
	remediationSteps, _ := args["remediation_steps"].(string)
	cvssBreakdown, _ := args["cvss_breakdown"].(string)

	// Optional fields
	endpoint, _ := args["endpoint"].(string)
	method, _ := args["method"].(string)
	cve, _ := args["cve"].(string)
	cwe, _ := args["cwe"].(string)

	// Validate required fields
	var errors []string
	if strings.TrimSpace(title) == "" {
		errors = append(errors, "Title cannot be empty")
	}
	if strings.TrimSpace(description) == "" {
		errors = append(errors, "Description cannot be empty")
	}
	if strings.TrimSpace(impact) == "" {
		errors = append(errors, "Impact cannot be empty")
	}
	if strings.TrimSpace(target) == "" {
		errors = append(errors, "Target cannot be empty")
	}
	if strings.TrimSpace(technicalAnalysis) == "" {
		errors = append(errors, "Technical analysis cannot be empty")
	}
	if strings.TrimSpace(pocDescription) == "" {
		errors = append(errors, "PoC description cannot be empty")
	}
	if strings.TrimSpace(pocScriptCode) == "" {
		errors = append(errors, "PoC script/code cannot be empty")
	}
	if strings.TrimSpace(remediationSteps) == "" {
		errors = append(errors, "Remediation steps cannot be empty")
	}

	if len(errors) > 0 {
		return map[string]interface{}{
			"success": false,
			"message": "Validation failed",
			"errors":  errors,
		}, nil
	}

	// Parse CVSS breakdown
	av, ac, pr, ui, s, c, i, a, validCVSS := extractCVSSValues(cvssBreakdown)
	if !validCVSS {
		errors = append(errors, "cvss_breakdown: could not parse all required CVSS3 fields (need: attack_vector, attack_complexity, privileges_required, user_interaction, scope, confidentiality, integrity, availability)")
	}

	if len(errors) > 0 {
		return map[string]interface{}{
			"success": false,
			"message": "Validation failed",
			"errors":  errors,
		}, nil
	}

	// Calculate CVSS3 base score
	cvssScore, severity := calculateCVSS3BaseScore(av, ac, pr, ui, s, c, i, a)

	// Build CVSS vector string
	cvssVector := fmt.Sprintf("CVSS:3.1/AV:%s/AC:%s/PR:%s/UI:%s/S:%s/C:%s/I:%s/A:%s",
		strings.ToUpper(av),
		strings.ToUpper(ac),
		strings.ToUpper(pr),
		strings.ToUpper(ui),
		strings.ToUpper(s),
		strings.ToUpper(c),
		strings.ToUpper(i),
		strings.ToUpper(a),
	)

	// Generate report ID
	reportID := generateReportID()

	// Create report
	report := &VulnerabilityReport{
		ReportID:          reportID,
		Title:             strings.TrimSpace(title),
		Description:       strings.TrimSpace(description),
		Impact:            strings.TrimSpace(impact),
		Target:            strings.TrimSpace(target),
		TechnicalAnalysis: strings.TrimSpace(technicalAnalysis),
		PocDescription:    strings.TrimSpace(pocDescription),
		PocScriptCode:     strings.TrimSpace(pocScriptCode),
		RemediationSteps:  strings.TrimSpace(remediationSteps),
		Endpoint:          strings.TrimSpace(endpoint),
		Method:            strings.TrimSpace(method),
		CVE:               strings.TrimSpace(cve),
		CWE:               strings.TrimSpace(cwe),
		CVSSScore:         cvssScore,
		CVSSSeverity:      severity,
		CVSSVector:        cvssVector,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	}

	// Store in memory
	reportsStorage[reportID] = report

	// Persist to JSONL
	appendReportEvent("create", reportID, report)

	slog.Info("Vulnerability report created",
		slog.String("report_id", reportID),
		slog.String("title", report.Title),
		slog.Float64("cvss_score", cvssScore),
		slog.String("severity", severity),
	)

	return map[string]interface{}{
		"success":     true,
		"message":     fmt.Sprintf("Vulnerability report '%s' created successfully", report.Title),
		"report_id":   reportID,
		"severity":    severity,
		"cvss_score":  cvssScore,
		"cvss_vector": cvssVector,
	}, nil
}
