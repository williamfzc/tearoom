// hc_template.go preflights the local hc template before any task is sent.
// It keeps automated launches behind the readiness contract hc actually enforces.
package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type hcTemplate struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Error   string `json:"error"`
}

func (h HCCLI) preflightTemplate(ctx context.Context, name, agentType string) error {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return errors.New("hc template name is invalid")
	}
	data, err := h.run(ctx, "templates", "--json")
	if err != nil {
		return fmt.Errorf("preflight hc template: %w", err)
	}
	var result struct {
		Templates []hcTemplate `json:"templates"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decode hc templates: %w", err)
	}
	var template *hcTemplate
	for i := range result.Templates {
		if result.Templates[i].Name == name {
			template = &result.Templates[i]
			break
		}
	}
	if template == nil {
		return fmt.Errorf("hc template %q is missing; launch refused", name)
	}
	if template.Error != "" {
		return fmt.Errorf("hc template %q is invalid: %s", name, template.Error)
	}
	if agentType == "codex" && !hasShellArgument(template.Command, "--no-daemon") {
		return fmt.Errorf("hc template %q must start Codex with --no-daemon; launch refused", name)
	}
	configDir, err := hcConfigHome()
	if err != nil {
		return err
	}
	path := filepath.Join(configDir, "herdr-core", "templates", name+".md")
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read hc template %q for readiness preflight: %w", name, err)
	}
	frontmatter, ok := hcFrontmatter(string(contents))
	if !ok {
		return fmt.Errorf("hc template %q has no valid frontmatter; launch refused", name)
	}
	var command, ready string
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "command":
			command = strings.TrimSpace(value)
		case "ready":
			ready = strings.TrimSpace(value)
		}
	}
	if command != template.Command {
		return fmt.Errorf("hc template %q changed during readiness preflight; launch refused", name)
	}
	switch ready {
	case "", "report":
	case "quiet":
		return fmt.Errorf("hc template %q uses ready: quiet; reliable agent status is required for automated delivery", name)
	default:
		return fmt.Errorf("hc template %q has unsupported readiness mode %q; launch refused", name, ready)
	}
	return nil
}

func hcFrontmatter(contents string) (string, bool) {
	contents = strings.ReplaceAll(contents, "\r\n", "\n")
	if !strings.HasPrefix(contents, "---\n") {
		return "", false
	}
	rest := strings.TrimPrefix(contents, "---\n")
	front, _, ok := strings.Cut(rest, "\n---")
	return front, ok
}

func hasShellArgument(command, wanted string) bool {
	for _, part := range strings.Fields(command) {
		if strings.Trim(part, `"'`) == wanted {
			return true
		}
	}
	return false
}

func hasCommandArgument(command []string, wanted string) bool {
	for _, part := range command {
		if part == wanted {
			return true
		}
	}
	return false
}

func hcConfigHome() (string, error) {
	if path := os.Getenv("XDG_CONFIG_HOME"); path != "" {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		if path := os.Getenv("APPDATA"); path != "" {
			return path, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find hc config directory: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}
