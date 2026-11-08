// hc_cli.go adapts the installed herdr-core CLI to the dispatcher boundary.
package collab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type HCCLI struct{ Binary string }

func NewHCRuntime(binary string) HCRuntime { return HCCLI{Binary: binary} }

type terminalList struct {
	Terminals []terminal `json:"terminals"`
}

type terminal struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type terminalStatus struct {
	State string `json:"state"`
}
type firstMessage struct {
	Sent    bool   `json:"sent"`
	Started *bool  `json:"started"`
	Reason  string `json:"reason"`
}

func (h HCCLI) PanelStatus(ctx context.Context, name string) (PanelStatus, error) {
	data, err := h.run(ctx, "ls", "--json")
	if err != nil {
		return PanelUnknown, err
	}
	var list terminalList
	if err := json.Unmarshal(data, &list); err != nil {
		return PanelUnknown, fmt.Errorf("decode hc terminal list: %w", err)
	}
	var found *terminal
	for i := range list.Terminals {
		if list.Terminals[i].Name == name {
			found = &list.Terminals[i]
			break
		}
	}
	if found == nil {
		return PanelMissing, nil
	}
	if found.State != "live" {
		return PanelMissing, nil
	}
	data, err = h.run(ctx, "get", "--json", name)
	if err != nil {
		return PanelUnknown, err
	}
	var details struct {
		Status *terminalStatus `json:"status"`
	}
	if err := json.Unmarshal(data, &details); err != nil {
		return PanelUnknown, fmt.Errorf("decode hc terminal status: %w", err)
	}
	if details.Status == nil {
		return PanelUnknown, nil
	}
	switch details.Status.State {
	case "idle":
		return PanelIdle, nil
	case "done":
		return PanelDone, nil
	case "working":
		return PanelWorking, nil
	case "blocked", "error":
		return PanelBlocked, nil
	default:
		return PanelUnknown, nil
	}
}

func (h HCCLI) EnsurePanel(ctx context.Context, request PanelRequest) (SubmitResult, error) {
	args := []string{"run", "--bg", "--json", "--no-parent", "--name", request.Name, "--cwd", request.Directory,
		"--label", "tearoom.room=" + stableLabel(request.Room),
		"--label", "tearoom.role=" + request.Role}
	task := strings.TrimSpace(request.Instructions + "\n\n" + request.Task)
	if request.Template != "" {
		if err := h.preflightTemplate(ctx, request.Template, request.AgentType); err != nil {
			return SubmitResult{}, err
		}
		args = append(args, request.Template, task)
		data, err := h.run(ctx, args...)
		if err != nil {
			return SubmitResult{}, err
		}
		var started struct {
			First *firstMessage `json:"first_message"`
		}
		if err := json.Unmarshal(data, &started); err != nil {
			return SubmitResult{}, fmt.Errorf("decode hc template launch: %w", err)
		}
		if started.First == nil {
			return SubmitResult{}, errors.New("hc template launch omitted first_message result")
		}
		return SubmitResult{Accepted: started.First.Sent, Started: started.First.Started}, nil
	}
	if len(request.Command) == 0 {
		return SubmitResult{}, errors.New("role has neither an hc template nor a command")
	}
	if request.AgentType == "codex" && !hasCommandArgument(request.Command, "--no-daemon") {
		return SubmitResult{}, errors.New("Codex command must include --no-daemon; launch refused")
	}
	args = append(args, "--")
	args = append(args, request.Command...)
	if _, err := h.run(ctx, args...); err != nil {
		return SubmitResult{}, err
	}
	status, err := h.PanelStatus(ctx, request.Name)
	if err != nil {
		return SubmitResult{}, err
	}
	if status != PanelIdle && status != PanelDone {
		return SubmitResult{Pending: true}, nil
	}
	return h.Submit(ctx, request.Name, task, status)
}

func (h HCCLI) Submit(ctx context.Context, panel, task string, observed PanelStatus) (SubmitResult, error) {
	if observed == PanelDone {
		current, err := h.PanelStatus(ctx, panel)
		if err != nil {
			return SubmitResult{}, err
		}
		switch current {
		case PanelIdle:
			observed = PanelIdle
		case PanelDone:
		default:
			return SubmitResult{Pending: true}, nil
		}
	}
	args := []string{"send", "--json"}
	if observed == PanelIdle {
		args = append(args, "--if", "idle")
	} else if observed != PanelDone {
		return SubmitResult{Pending: true}, nil
	}
	// In single-dispatcher mode this narrows, but does not close, the done-to-working race.
	// hc send without --if is not atomic with the read above; no --force means hc still
	// refuses input if the panel is blocked at send time.
	args = append(args, panel, task)
	data, err := h.run(ctx, args...)
	if err != nil {
		return SubmitResult{}, err
	}
	var result struct {
		Started *bool `json:"started"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return SubmitResult{}, fmt.Errorf("decode hc send result: %w", err)
	}
	accepted := result.Started != nil && *result.Started
	return SubmitResult{Accepted: accepted, Started: result.Started}, nil
}

func (h HCCLI) run(ctx context.Context, args ...string) ([]byte, error) {
	binary := h.Binary
	if binary == "" {
		binary = "hc"
	}
	output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("hc %s: %w: %s", args[0], err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func stableLabel(value string) string {
	// Labels are descriptive only; the terminal name is the routing identity.
	if len(value) <= 32 && !strings.ContainsAny(value, "=,\n\r") {
		return value
	}
	h := sha256.Sum256([]byte(value))
	return "lbl-" + hex.EncodeToString(h[:12])
}
