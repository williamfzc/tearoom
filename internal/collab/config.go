// config.go owns reusable role definitions and Room-role bindings for the
// collaboration layer. This metadata stays outside the Room record stream.
package collab

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"
)

const configName = "collaboration.json"

// Config is the workspace collaboration roster and reusable role catalog.
type Config struct {
	Roles map[string]Role `json:"roles"`
	Rooms map[string]Room `json:"rooms"`
}

// Role defines how one kind of collaborator is described and started.
type Role struct {
	Description  string   `json:"description"`
	AgentType    string   `json:"agent_type"`
	Template     string   `json:"template,omitempty"`
	Command      []string `json:"command"`
	Directory    string   `json:"directory,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
}

// Room lists the role definitions configured for one Room.
type Room struct {
	Roles []string `json:"roles"`
}

// Message is structured addressed content. An empty Mentions list is a normal
// broadcast message; ordinary string content remains valid for older records.
type Message struct {
	Text     string     `json:"text"`
	Mentions []string   `json:"mentions,omitempty"`
	ReplyTo  *RecordRef `json:"reply_to,omitempty"`
}

// RecordRef identifies a message whose work this Room record completes.
type RecordRef struct {
	Room string `json:"room"`
	Seq  uint64 `json:"seq"`
}

// Load reads collaboration metadata from the .tearoom directory. Missing
// metadata means an empty catalog; no existing workspace data is changed.
func Load(root string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(root, configName))
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", configName, err)
	}
	if config.Roles == nil {
		config.Roles = map[string]Role{}
	}
	if config.Rooms == nil {
		config.Rooms = map[string]Room{}
	}
	return config, nil
}

// Empty returns a writable empty collaboration catalog.
func Empty() Config {
	return Config{Roles: map[string]Role{}, Rooms: map[string]Room{}}
}

// Update locks collaboration metadata across processes and atomically saves
// the result of change. It prevents lost updates between CLI and dispatcher.
func Update(root string, change func(*Config) error) error {
	lock, err := os.OpenFile(filepath.Join(root, "collaboration.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	config, err := Load(root)
	if err != nil {
		return err
	}
	if err := change(&config); err != nil {
		return err
	}
	if err := Validate(config); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(root, ".collaboration-*.tmp")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(path, filepath.Join(root, configName)); err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Validate checks references and names without treating the roster as access
// control or interpreting agent commands.
func Validate(config Config) error {
	for name, role := range config.Roles {
		if !ValidRoleName(name) {
			return fmt.Errorf("invalid role name %q", name)
		}
		if strings.TrimSpace(role.AgentType) == "" {
			return fmt.Errorf("role %q requires an agent type", name)
		}
		hasCommand := len(role.Command) > 0 && strings.TrimSpace(role.Command[0]) != ""
		hasTemplate := strings.TrimSpace(role.Template) != ""
		if hasCommand == hasTemplate {
			return fmt.Errorf("role %q requires exactly one of command or template", name)
		}
	}
	for roomName, room := range config.Rooms {
		if !validName(roomName) {
			return fmt.Errorf("invalid room name %q in collaboration config", roomName)
		}
		seen := map[string]bool{}
		for _, name := range room.Roles {
			if _, ok := config.Roles[name]; !ok {
				return fmt.Errorf("room %q refers to unknown role %q", roomName, name)
			}
			if seen[name] {
				return fmt.Errorf("room %q lists role %q more than once", roomName, name)
			}
			seen[name] = true
		}
	}
	return nil
}

// SortedRoles returns the configured roles in stable display order.
func (room Room) SortedRoles() []string {
	roles := append([]string(nil), room.Roles...)
	sort.Strings(roles)
	return roles
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return false
	}
	return !strings.ContainsAny(name, "/\\\x00")
}

// ValidRoleName checks the stable identifier syntax accepted by mentions.
func ValidRoleName(name string) bool {
	if name == "" {
		return false
	}
	for index, value := range name {
		valid := unicode.IsLetter(value) || unicode.IsDigit(value)
		if index > 0 {
			valid = valid || value == '_' || value == '-'
		}
		if !valid {
			return false
		}
	}
	return true
}
