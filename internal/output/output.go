package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

// Format represents an output format.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
	FormatSARIF Format = "sarif"
)

// Options controls where and how results are written.
type Options struct {
	Format     Format
	OutputPath string // empty = stdout
}

// Write renders compliance results using the given options.
// If OutputPath is set, output goes to that file; otherwise to w.
func Write(w io.Writer, results []compliance.Result, opts Options) error {
	dest := w
	if opts.OutputPath != "" {
		f, err := os.Create(opts.OutputPath)
		if err != nil {
			return fmt.Errorf("creating output file %s: %w", opts.OutputPath, err)
		}
		defer f.Close()
		dest = f
	}

	switch opts.Format {
	case FormatJSON:
		return writeJSON(dest, results)
	case FormatCSV:
		return writeCSV(dest, results)
	case FormatSARIF:
		return writeSARIF(dest, results)
	case FormatTable, "":
		return writeTable(dest, results)
	default:
		return fmt.Errorf("unknown output format: %s", opts.Format)
	}
}

func writeTable(w io.Writer, results []compliance.Result) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "STATUS\tSEVERITY\tRULE\tREPO\tMESSAGE\n")
	fmt.Fprintf(tw, "------\t--------\t----\t----\t-------\n")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Status, r.Severity, r.RuleID, r.Repo, r.Message)
	}
	return tw.Flush()
}

func writeJSON(w io.Writer, results []compliance.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func writeCSV(w io.Writer, results []compliance.Result) error {
	fmt.Fprintf(w, "status,severity,rule_id,rule_name,repo,message\n")
	for _, r := range results {
		fmt.Fprintf(w, "%s,%s,%s,%s,%s,%s\n",
			csvEscape(string(r.Status)),
			csvEscape(string(r.Severity)),
			csvEscape(r.RuleID),
			csvEscape(r.RuleName),
			csvEscape(r.Repo),
			csvEscape(r.Message),
		)
	}
	return nil
}

// csvEscape wraps a field in double-quotes if it contains a comma, quote, or newline.
func csvEscape(s string) string {
	needsQuoting := false
	for _, c := range s {
		if c == ',' || c == '"' || c == '\n' || c == '\r' {
			needsQuoting = true
			break
		}
	}
	if !needsQuoting {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		if c == '"' {
			b.WriteString(`""`)
		} else {
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// HasFailures returns true if any result has a fail status with error severity.
func HasFailures(results []compliance.Result) bool {
	for _, r := range results {
		if r.Status == compliance.StatusFail && r.Severity == config.SeverityError {
			return true
		}
	}
	return false
}
