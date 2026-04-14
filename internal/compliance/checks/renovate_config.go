package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

const (
	defaultRenovateExtends = "github>reearth/renovate-config"
	defaultMinStabilityDays = "7"
)

type renovateConfigChecker struct{}

func (c *renovateConfigChecker) ID() string { return "renovate-config" }

func (c *renovateConfigChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	requiredExtends := rule.Params["extends"]
	if requiredExtends == "" {
		requiredExtends = defaultRenovateExtends
	}
	requiredDays := rule.Params["min_stability_days"]
	if requiredDays == "" {
		requiredDays = defaultMinStabilityDays
	}

	// Check common renovate config locations
	configPaths := []string{
		"renovate.json",
		"renovate.json5",
		".github/renovate.json",
		".github/renovate.json5",
		".renovaterc",
		".renovaterc.json",
	}

	var content []byte
	var foundPath string
	for _, path := range configPaths {
		c, err := gh.FetchFileContent(ctx, client, repo.Owner, repo.Name, path, ref)
		if err != nil {
			return nil, err
		}
		if c != nil {
			content = c
			foundPath = path
			break
		}
	}

	if content == nil {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  "no renovate configuration found",
		}, nil
	}

	// Parse the JSON config
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(content, &cfg); err != nil {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("invalid JSON in %s: %v", foundPath, err),
		}, nil
	}

	// Check "extends" contains the required preset
	var failures []string

	extendsRaw, ok := cfg["extends"]
	if !ok {
		failures = append(failures, fmt.Sprintf("missing 'extends' in %s", foundPath))
	} else {
		var extends []string
		if err := json.Unmarshal(extendsRaw, &extends); err != nil {
			failures = append(failures, fmt.Sprintf("'extends' is not an array in %s", foundPath))
		} else {
			found := false
			for _, e := range extends {
				if strings.Contains(e, requiredExtends) {
					found = true
					break
				}
			}
			if !found {
				failures = append(failures, fmt.Sprintf("'extends' does not include %q", requiredExtends))
			}
		}
	}

	// Check stabilityDays / minimumReleaseAge
	if stabilityRaw, ok := cfg["stabilityDays"]; ok {
		var days int
		if err := json.Unmarshal(stabilityRaw, &days); err == nil {
			requiredDaysInt := 7
			fmt.Sscanf(requiredDays, "%d", &requiredDaysInt)
			if days < requiredDaysInt {
				failures = append(failures, fmt.Sprintf("stabilityDays=%d < required %s", days, requiredDays))
			}
		}
	} else if minAgeRaw, ok := cfg["minimumReleaseAge"]; ok {
		// minimumReleaseAge is the newer field (e.g. "7 days")
		var minAge string
		if err := json.Unmarshal(minAgeRaw, &minAge); err == nil {
			if !strings.Contains(minAge, requiredDays) {
				failures = append(failures, fmt.Sprintf("minimumReleaseAge=%q may not satisfy %s day cooldown", minAge, requiredDays))
			}
		}
	}
	// Note: if neither stabilityDays nor minimumReleaseAge is present, the extends
	// preset may already configure it, so we don't fail on that alone.

	if len(failures) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  strings.Join(failures, "; "),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  fmt.Sprintf("renovate config OK (%s)", foundPath),
	}, nil
}

func init() {
	compliance.Register(&renovateConfigChecker{})
}
