package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

// SARIF 2.1.0 schema — subset sufficient for GitHub Code Scanning upload.
// https://docs.github.com/en/code-security/code-scanning/integrating-with-code-scanning/sarif-support-for-code-scanning

const sarifSchema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"
const sarifVersion = "2.1.0"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool    `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	ShortDescription sarifMessage         `json:"shortDescription"`
	DefaultConfig    sarifRuleDefaultConf `json:"defaultConfiguration"`
}

type sarifRuleDefaultConf struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID  string          `json:"ruleId"`
	Level   string          `json:"level"`
	Message sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

// severityToSARIFLevel maps compliance severities to SARIF levels.
func severityToSARIFLevel(s config.Severity) string {
	switch s {
	case config.SeverityError:
		return "error"
	case config.SeverityWarning:
		return "warning"
	default:
		return "note"
	}
}

func writeSARIF(w io.Writer, results []compliance.Result) error {
	// Build deduplicated rule list
	seenRules := map[string]bool{}
	var rules []sarifRule
	for _, r := range results {
		if seenRules[r.RuleID] {
			continue
		}
		seenRules[r.RuleID] = true
		rules = append(rules, sarifRule{
			ID:               r.RuleID,
			Name:             r.RuleName,
			ShortDescription: sarifMessage{Text: r.RuleName},
			DefaultConfig:    sarifRuleDefaultConf{Level: severityToSARIFLevel(r.Severity)},
		})
	}

	var sarifResults []sarifResult
	for _, r := range results {
		if r.Status != compliance.StatusFail {
			continue // only emit failures
		}
		sarifResults = append(sarifResults, sarifResult{
			RuleID:  r.RuleID,
			Level:   severityToSARIFLevel(r.Severity),
			Message: sarifMessage{Text: fmt.Sprintf("[%s] %s", r.Repo, r.Message)},
			Locations: []sarifLocation{
				{
					PhysicalLocation: sarifPhysicalLocation{
						ArtifactLocation: sarifArtifactLocation{
							URI: fmt.Sprintf("https://github.com/%s", r.Repo),
						},
					},
				},
			},
		})
	}

	log := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:           "git-cascade",
						Version:        "1.0.0",
						InformationURI: "https://github.com/eukarya-inc/git-cascade",
						Rules:          rules,
					},
				},
				Results: sarifResults,
			},
		},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
