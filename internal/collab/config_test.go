package collab

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestUpdatePreservesConcurrentRoleDefinitionsAndRoomBindings(t *testing.T) {
	root := t.TempDir()
	var group sync.WaitGroup
	for _, name := range []string{"builder", "reviewer"} {
		name := name
		group.Add(1)
		go func() {
			defer group.Done()
			if err := Update(root, func(config *Config) error {
				config.Roles[name] = Role{AgentType: "test", Command: []string{"agent"}}
				config.Rooms["project"] = Room{Roles: append(config.Rooms["project"].Roles, name)}
				return nil
			}); err != nil {
				t.Errorf("update %s: %v", name, err)
			}
		}()
	}
	group.Wait()
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roles) != 2 || len(got.Rooms["project"].Roles) != 2 {
		t.Fatalf("config lost an update: %+v", got)
	}
	info, err := os.Stat(filepath.Join(root, configName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestValidateRejectsUnknownRoleReferenceAndInvalidRoleName(t *testing.T) {
	for _, config := range []Config{
		{Roles: map[string]Role{"builder": {AgentType: "codex", Command: []string{"codex"}}}, Rooms: map[string]Room{"project": {Roles: []string{"reviewer"}}}},
		{Roles: map[string]Role{"bad/name": {AgentType: "codex", Command: []string{"codex"}}}},
		{Roles: map[string]Role{"builder": {AgentType: "codex", Command: []string{"codex"}, Template: "builder"}}},
	} {
		if err := Validate(config); err == nil {
			t.Fatalf("Validate(%+v) succeeded", config)
		}
	}
}

func TestLoadMissingConfigIsReadOnlyEmptyCatalog(t *testing.T) {
	root := t.TempDir()
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roles) != 0 || len(got.Rooms) != 0 {
		t.Fatalf("config = %+v, want empty", got)
	}
	if _, err := os.Stat(filepath.Join(root, configName)); !os.IsNotExist(err) {
		t.Fatalf("Load created a config file: %v", err)
	}
}
