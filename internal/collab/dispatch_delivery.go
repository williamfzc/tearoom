// dispatch_delivery.go derives addressed intents and serializes each Room-role queue.
package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/williamfzc/tearoom/internal/core"
)

func (d *Dispatcher) isHeadOfRoleQueue(state dispatchState, intentIndex, deliveryIndex int) bool {
	current := state.Intents[intentIndex]
	role := current.Deliveries[deliveryIndex].Role
	for i := 0; i < intentIndex; i++ {
		prior := state.Intents[i]
		if prior.Room != current.Room || prior.Seq >= current.Seq {
			continue
		}
		for _, delivery := range prior.Deliveries {
			if delivery.Role == role && delivery.State != DeliveryCompleted && delivery.State != DeliverySkipped {
				return false
			}
		}
	}
	return true
}

func (d *Dispatcher) markReply(state *dispatchState, record core.Record) {
	var message Message
	if json.Unmarshal(record.Content, &message) != nil || message.ReplyTo == nil || record.Author == "" {
		return
	}
	if record.Room != message.ReplyTo.Room {
		return
	}
	for i := range state.Intents {
		intent := &state.Intents[i]
		if intent.Room != message.ReplyTo.Room || intent.Seq != message.ReplyTo.Seq {
			continue
		}
		for j := range intent.Deliveries {
			delivery := &intent.Deliveries[j]
			if delivery.Role == record.Author && delivery.State == DeliverySubmitted {
				delivery.State = DeliveryCompleted
				delivery.Error = ""
				return
			}
		}
	}
}

func (d *Dispatcher) recordIntent(state *dispatchState, config Config, room string, record core.Record) error {
	var message Message
	if err := json.Unmarshal(record.Content, &message); err != nil || message.Text == "" {
		// Plain strings and unrelated JSON remain ordinary broadcast records.
		return nil
	}
	if len(message.Mentions) == 0 {
		return nil
	}
	for _, existing := range state.Intents {
		if existing.Room == room && existing.Seq == record.Seq {
			return nil
		}
	}
	roomConfig := config.Rooms[room]
	allowed := make(map[string]bool, len(roomConfig.Roles))
	for _, role := range roomConfig.Roles {
		allowed[role] = true
	}
	roles := append([]string(nil), message.Mentions...)
	sort.Strings(roles)
	deliveries := make([]Delivery, 0, len(roles))
	for i, role := range roles {
		if role == "" || (i > 0 && roles[i-1] == role) {
			continue
		}
		if !allowed[role] {
			panel, err := PanelName(d.root, room, role)
			if err != nil {
				return err
			}
			deliveries = append(deliveries, Delivery{Role: role, Panel: panel, State: DeliveryUnknown,
				Error: "target role is not configured in this Room"})
			continue
		}
		if _, ok := config.Roles[role]; !ok {
			panel, err := PanelName(d.root, room, role)
			if err != nil {
				return err
			}
			deliveries = append(deliveries, Delivery{Role: role, Panel: panel, State: DeliveryUnknown,
				Error: "target role definition is missing"})
			continue
		}
		panel, err := PanelName(d.root, room, role)
		if err != nil {
			return err
		}
		deliveries = append(deliveries, Delivery{Role: role, Panel: panel, State: DeliveryPending})
	}
	if len(deliveries) == 0 {
		return nil
	}
	state.Intents = append(state.Intents, Intent{Room: room, Seq: record.Seq, Text: message.Text, Deliveries: deliveries})
	return nil
}

func (d *Dispatcher) deliver(ctx context.Context, config Config, intent *Intent, delivery *Delivery) error {
	if delivery.State != DeliveryPending {
		return nil
	}
	expectedPanel, err := PanelName(d.root, intent.Room, delivery.Role)
	if err != nil {
		return err
	}
	if delivery.Panel != expectedPanel {
		delivery.State = DeliveryUnknown
		delivery.Error = "stored panel predates workspace-scoped identity; inspect it, then explicitly resolve before retry"
		return nil
	}
	role := config.Roles[delivery.Role]
	status, err := d.hc.PanelStatus(ctx, delivery.Panel)
	if err != nil {
		return d.markUnknown(delivery, err)
	}
	cli, err := os.Executable()
	if err != nil {
		cli = "tearoom"
	}
	roomArg := shellQuote(intent.Room)
	cliArg := shellQuote(cli)
	task := fmt.Sprintf("You are the %s role only. Do not perform, impersonate, or write under any other role, and do not review your own work. After handing off to another role, end your turn and wait. Use the Tearoom CLI at %s. Read Room %s with %s read --room %s. Handle record e%d there. Write your result or blocker into that same Room with --as %s and --reply-to %s#%d (for example: %s send --room %s --as %s --reply-to %s#%d --text 'done: ...'). The author label is a collaboration convention, not authenticated identity.",
		delivery.Role, cliArg, roomArg, cliArg, roomArg, intent.Seq,
		delivery.Role, roomArg, intent.Seq, cliArg, roomArg, delivery.Role, roomArg, intent.Seq)
	request := PanelRequest{Name: delivery.Panel, Room: intent.Room, Role: delivery.Role,
		AgentType: role.AgentType, Template: role.Template, Command: role.Command,
		Directory: role.Directory, Instructions: role.Instructions, Task: task}
	workspaceRoot, err := canonicalWorkspaceRoot(filepath.Dir(d.root))
	if err != nil {
		return err
	}
	if request.Directory == "" {
		request.Directory = workspaceRoot
	} else if !filepath.IsAbs(request.Directory) {
		request.Directory = filepath.Join(workspaceRoot, request.Directory)
	}
	switch status {
	case PanelMissing:
		delivery.State = DeliverySubmitting
		delivery.Error = ""
		if err := d.markSubmitting(intent, delivery); err != nil {
			return err
		}
		result, err := d.hc.EnsurePanel(ctx, request)
		if err != nil {
			return d.markUnknown(delivery, err)
		}
		if result.Pending {
			delivery.State = DeliveryPending
			delivery.Error = "panel started but has not reported idle; task remains queued"
			return nil
		}
		if !result.Accepted || (result.Started != nil && !*result.Started) {
			return d.markUnknown(delivery, errors.New("hc created or found a panel but did not confirm its first task was sent"))
		}
		delivery.State = DeliverySubmitted
	case PanelIdle, PanelDone:
		delivery.State = DeliverySubmitting
		delivery.Error = ""
		if err := d.markSubmitting(intent, delivery); err != nil {
			return err
		}
		result, err := d.hc.Submit(ctx, delivery.Panel, task, status)
		if err != nil {
			return d.markUnknown(delivery, err)
		}
		if result.Pending {
			delivery.State = DeliveryPending
			delivery.Error = "panel status changed during delivery preflight; task remains queued"
			return nil
		}
		if !result.Accepted || (result.Started != nil && !*result.Started) {
			return d.markUnknown(delivery, errors.New("hc did not confirm that the task started"))
		}
		delivery.State = DeliverySubmitted
	case PanelWorking, PanelBlocked, PanelUnknown:
		// Wait for a later Pump after the role becomes usable; never force input.
		delivery.Error = map[PanelStatus]string{
			PanelWorking: "panel is working; task remains queued",
			PanelBlocked: "panel is blocked; inspect it before resolving delivery",
			PanelUnknown: "panel has not reported idle; task remains queued",
		}[status]
		return nil
	default:
		return d.markUnknown(delivery, fmt.Errorf("unsupported hc panel status %q", status))
	}
	delivery.Error = ""
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// markSubmitting persists the delivery before any external hc side effect.
// Pump holds the workspace singleton lock around this read-modify-write path.
