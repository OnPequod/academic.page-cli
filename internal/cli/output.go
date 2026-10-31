package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/OnPequod/academic.page-cli/internal/app"
	"github.com/OnPequod/academic.page-cli/internal/registry"
)

func (c *CLI) printResult(rc *registry.Command, call app.Call, res *app.Result) error {
	switch {
	case res.Plan != nil:
		if c.json {
			return c.printJSON(res.Plan)
		}
		fmt.Fprintf(c.Stdout, "Dry run, nothing sent.\n%s %s\n", res.Plan.Method, res.Plan.URL)
		if res.Plan.File != nil {
			fmt.Fprintf(c.Stdout, "File: %s (%d bytes)\n", res.Plan.File.Path, res.Plan.File.Size)
		}
		if res.Plan.Body != nil {
			return c.printJSON(res.Plan.Body)
		}
		return nil
	case res.Text != "":
		if c.json {
			return c.printJSON(map[string]string{"text": res.Text})
		}
		_, err := io.WriteString(c.Stdout, res.Text)
		return err
	}

	if c.json {
		var buf bytes.Buffer
		if err := json.Indent(&buf, res.JSON, "", "  "); err != nil {
			return err
		}
		buf.WriteByte('\n')
		_, err := c.Stdout.Write(buf.Bytes())
		return err
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
		Meta *struct {
			CurrentPage int `json:"current_page"`
			LastPage    int `json:"last_page"`
			Total       int `json:"total"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(res.JSON, &envelope)

	if rc.Kind != registry.KindRead {
		return c.printWriteSummary(rc, call, res.JSON, envelope.Data)
	}
	var rows []map[string]any
	if len(envelope.Data) > 0 && envelope.Data[0] == '[' && json.Unmarshal(envelope.Data, &rows) == nil {
		c.printTable(rows)
		if m := envelope.Meta; m != nil && m.LastPage > 1 {
			fmt.Fprintf(c.Stdout, "\nPage %d of %d, %d in all. Use --page or --all for more.\n", m.CurrentPage, m.LastPage, m.Total)
		}
		return nil
	}
	return c.printJSON(json.RawMessage(res.JSON))
}

func (c *CLI) printWriteSummary(rc *registry.Command, call app.Call, raw, data json.RawMessage) error {
	if rc.Kind == registry.KindDelete {
		if call.Arg == "" {
			fmt.Fprintf(c.Stdout, "Deleted the %s.\n", singular(rc.Group))
		} else {
			fmt.Fprintf(c.Stdout, "Deleted %s %s.\n", singular(rc.Group), call.Arg)
		}
		return nil
	}
	if len(data) == 0 {
		return c.printJSON(json.RawMessage(raw))
	}
	var record map[string]any
	if json.Unmarshal(data, &record) != nil {
		return c.printJSON(json.RawMessage(raw))
	}
	verb := map[string]string{"create": "Created", "update": "Updated", "upload": "Uploaded"}[rc.Name]
	if verb == "" {
		// Imports, reorders and suggestion actions return more than one
		// record's worth; show the response.
		return c.printJSON(json.RawMessage(raw))
	}
	line := fmt.Sprintf("%s %s %v", verb, singular(rc.Group), record["id"])
	if title := label(record); title != "" {
		line += fmt.Sprintf(" %q", title)
	}
	if rc.Publishable {
		if p, ok := record["published_at"]; ok && p != nil {
			line += fmt.Sprintf(" (published %v)", p)
		} else {
			line += " (draft)"
		}
	}
	fmt.Fprintln(c.Stdout, line+".")
	return nil
}

func (c *CLI) printTable(rows []map[string]any) {
	if len(rows) == 0 {
		fmt.Fprintln(c.Stdout, "No records.")
		return
	}
	w := tabwriter.NewWriter(c.Stdout, 0, 0, 2, ' ', 0)
	cols := columns(rows[0])
	fmt.Fprintln(w, strings.ToUpper(strings.Join(cols, "\t")))
	for _, row := range rows {
		cells := make([]string, len(cols))
		for i, col := range cols {
			cells[i] = cell(row, col)
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	_ = w.Flush()
}

var labelKeys = []string{"title", "name", "filename", "file_name", "label"}

func columns(row map[string]any) []string {
	cols := []string{}
	for _, k := range []string{"id"} {
		if _, ok := row[k]; ok {
			cols = append(cols, k)
		}
	}
	for _, k := range labelKeys {
		if _, ok := row[k]; ok {
			cols = append(cols, k)
			break
		}
	}
	for _, k := range []string{"slug", "published_at", "updated_at"} {
		if _, ok := row[k]; ok {
			cols = append(cols, k)
		}
	}
	if len(cols) <= 1 {
		keys := make([]string, 0, len(row))
		for k := range row {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if len(cols) >= 4 {
				break
			}
			switch row[k].(type) {
			case string, float64, bool:
				if k != "id" {
					cols = append(cols, k)
				}
			}
		}
	}
	return cols
}

func label(record map[string]any) string {
	for _, k := range labelKeys {
		if s, ok := record[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func cell(row map[string]any, col string) string {
	v, ok := row[col]
	if !ok || v == nil {
		return "-"
	}
	var s string
	switch v := v.(type) {
	case float64:
		s = fmt.Sprintf("%.0f", v)
		if float64(int64(v)) != v {
			s = fmt.Sprint(v)
		}
	case string:
		s = v
	default:
		b, _ := json.Marshal(v)
		s = string(b)
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:59]) + "…"
	}
	return s
}

func singular(group string) string {
	switch group {
	case "media":
		return "media file"
	case "study":
		return "study"
	case "profile-cv":
		return "profile CV"
	}
	return strings.TrimSuffix(strings.ReplaceAll(group, "-", " "), "s")
}

func readAll(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("no standard input")
	}
	return io.ReadAll(r)
}

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }
