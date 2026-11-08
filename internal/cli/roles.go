// roles.go exposes the workspace role catalog and the Room roster. These
// definitions guide hc launch and routing; they do not grant Room access.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/williamfzc/tearoom/internal/collab"
	"github.com/williamfzc/tearoom/internal/store"
)

const roleHelp = `Define or inspect reusable collaborator roles.

Usage:
  tearoom role define --name <name> --agent-type <type> (--template <name> | --command <program>) [flags]
  tearoom role list
  tearoom role show --name <name>

Flags for define:
  --description string    what this role does
  --agent-type string     coding agent or runtime adapter name
  --command string        executable; pass each argument with --arg
  --template string       installed hc template name
  --arg string            executable argument; repeatable
  --cwd string            default working directory; defaults to workspace root
  --instructions string   role instructions supplied at launch
`

func (a *App) runRole(args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(a.Out, roleHelp)
		return nil
	}
	switch args[0] {
	case "define":
		return a.defineRole(args[1:])
	case "list":
		return a.listRoles(args[1:])
	case "show":
		return a.showRole(args[1:])
	default:
		return fmt.Errorf("unknown role command %q", args[0])
	}
}

func (a *App) defineRole(args []string) error {
	flags := newFlags("role define", a.Out, roleHelp)
	name := flags.String("name", "", "")
	description := flags.String("description", "", "")
	agentType := flags.String("agent-type", "", "")
	command := flags.String("command", "", "")
	template := flags.String("template", "", "")
	cwd := flags.String("cwd", "", "")
	instructions := flags.String("instructions", "", "")
	var argv stringsFlag
	flags.Var(&argv, "arg", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return errors.New("--name is required")
	}
	if (strings.TrimSpace(*command) == "") == (strings.TrimSpace(*template) == "") {
		return errors.New("provide exactly one of --command and --template")
	}
	if strings.TrimSpace(*agentType) == "" {
		return errors.New("--agent-type is required")
	}
	root, err := store.Find(a.Dir)
	if err != nil {
		return err
	}
	directory := strings.TrimSpace(*cwd)
	var executable []string
	if strings.TrimSpace(*command) != "" {
		executable = append([]string{*command}, argv...)
	} else if len(argv) > 0 {
		return errors.New("--arg requires --command")
	}
	entry := collab.Role{
		Description:  strings.TrimSpace(*description),
		AgentType:    strings.TrimSpace(*agentType),
		Command:      executable,
		Template:     strings.TrimSpace(*template),
		Directory:    directory,
		Instructions: *instructions,
	}
	if err := collab.Update(root, func(config *collab.Config) error {
		config.Roles[*name] = entry
		return nil
	}); err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(map[string]any{"name": *name, "role": entry})
}

func (a *App) listRoles(args []string) error {
	flags := newFlags("role list", a.Out, roleHelp)
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	config, err := a.roleConfig()
	if err != nil {
		return err
	}
	for _, name := range sortedRoleNames(config.Roles) {
		if err := json.NewEncoder(a.Out).Encode(map[string]any{"name": name, "role": config.Roles[name]}); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) showRole(args []string) error {
	flags := newFlags("role show", a.Out, roleHelp)
	name := flags.String("name", "", "")
	if err := parseNoArgs(flags, args); err != nil {
		return err
	}
	if *name == "" {
		return errors.New("--name is required")
	}
	config, err := a.roleConfig()
	if err != nil {
		return err
	}
	role, ok := config.Roles[*name]
	if !ok {
		return fmt.Errorf("unknown role %q", *name)
	}
	return json.NewEncoder(a.Out).Encode(map[string]any{"name": *name, "role": role})
}

func (a *App) roleConfig() (collab.Config, error) {
	root, err := store.Find(a.Dir)
	if err != nil {
		return collab.Config{}, err
	}
	return collab.Load(root)
}

func sortedRoleNames(roles map[string]collab.Role) []string {
	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// parseMentions accepts role identifiers with an optional @ prefix.
func parseMentions(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	var names []string
	for _, item := range strings.Split(value, ",") {
		name := strings.TrimPrefix(strings.TrimSpace(item), "@")
		if !collab.ValidRoleName(name) {
			return nil, fmt.Errorf("invalid role mention %q", item)
		}
		if seen[name] {
			return nil, fmt.Errorf("role %q mentioned more than once", name)
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}

type stringsFlag []string

func (f *stringsFlag) String() string { return strings.Join(*f, ",") }
func (f *stringsFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
func (*stringsFlag) IsBoolFlag() bool { return false }
