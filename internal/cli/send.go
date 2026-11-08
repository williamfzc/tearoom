// send.go owns append command parsing and its Room-addressed message shape.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/williamfzc/tearoom/internal/collab"
	"github.com/williamfzc/tearoom/internal/core"
	"github.com/williamfzc/tearoom/internal/store"
)

const sendHelp = `Append one record to a room.

Plain text is encoded as a JSON string unless it contains an explicit @role
mention or uses --to/--reply-to; --json passes the raw JSON content.
The appended record is printed as one JSON object.

Usage:
  tearoom send [--room <room>] <text> [flags]
  tearoom send [--room <room>] --text <text> [flags]
  tearoom send [--room <room>] --json <json> [flags]

Flags:
  --room string             target room; defaults to the only room of the workspace
  --as string               signature label; defaults to TEAROOM_ACTOR, may be empty
  --to string               explicit @role target(s), comma-separated
  --reply-to string         work receipt target as <room>#<seq>
  --text string             the record text
  --json string             raw JSON content instead of text
  --idempotency-key string  retry identity; an equal retry returns the original record
  -h, --help                show help
`

func (a *App) runSend(args []string) error {
	flags := newFlags("send", a.Out, sendHelp)
	roomFlag := flags.String("room", "", "")
	as := flags.String("as", "", "")
	text := flags.String("text", "", "")
	raw := flags.String("json", "", "")
	key := flags.String("idempotency-key", "", "")
	to := flags.String("to", "", "")
	replyTo := flags.String("reply-to", "", "")
	if err := parse(flags, args); err != nil {
		return err
	}
	positionals := strings.Join(flags.Args(), " ")
	var content json.RawMessage
	var plain string
	var hasText bool
	switch {
	case countSet(*raw, *text, positionals) > 1:
		return errors.New("provide exactly one of positional text, --text, or --json")
	case *raw != "":
		if *to != "" || *replyTo != "" {
			return errors.New("--to and --reply-to require text content; addressed --json is not supported")
		}
		content = json.RawMessage(*raw)
	case *text != "":
		plain, hasText = *text, true
	case positionals != "":
		plain, hasText = positionals, true
	default:
		return errors.New("provide positional text, --text, or --json content")
	}
	st, err := a.openStore()
	if err != nil {
		return err
	}
	ctx := a.context()
	room, err := a.resolveRoom(ctx, st, *roomFlag)
	if err != nil {
		return err
	}
	if hasText {
		bodyMentions := extractRoleMentions(plain)
		mentions, err := parseMentions(*to)
		if err != nil {
			return err
		}
		mentions = mergeMentions(bodyMentions, mentions)
		for _, name := range mentions {
			if name == "all" {
				return errors.New("@all is not supported; mention a configured role explicitly")
			}
		}
		var reply *collab.RecordRef
		if *replyTo != "" {
			reply, err = parseRecordRef(*replyTo)
			if err != nil {
				return err
			}
		}
		if len(mentions) > 0 {
			root, err := store.Find(a.Dir)
			if err != nil {
				return err
			}
			config, err := collab.Load(root)
			if err != nil {
				return err
			}
			configured, ok := config.Rooms[room]
			if !ok {
				return fmt.Errorf("room %q has no collaborator roles configured", room)
			}
			available := map[string]bool{}
			for _, name := range configured.Roles {
				available[name] = true
			}
			for _, name := range mentions {
				if !available[name] {
					return fmt.Errorf("role %q is not configured in room %q", name, room)
				}
			}
		}
		if len(mentions) > 0 || reply != nil {
			content, err = json.Marshal(collab.Message{Text: plain, Mentions: mentions, ReplyTo: reply})
			if err != nil {
				return err
			}
		} else {
			content, err = json.Marshal(plain)
			if err != nil {
				return err
			}
		}
	}
	record, err := st.Append(ctx, room, signature(*as), core.AppendRequest{
		Content:        content,
		IdempotencyKey: *key,
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(a.Out).Encode(record)
}

// extractRoleMentions finds explicit @role references in prose. It ignores
// markdown code spans and fences, escaped at-signs, and email addresses.
func extractRoleMentions(text string) []string {
	var mentions []string
	seen := map[string]bool{}
	lines := strings.SplitAfter(text, "\n")
	insideFence := false
	var fence byte
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if marker, ok := markdownFence(trimmed); ok {
			if !insideFence {
				insideFence, fence = true, marker
			} else if marker == fence {
				insideFence = false
			}
			continue
		}
		if insideFence {
			continue
		}
		for i := 0; i < len(line); {
			if line[i] == '`' {
				run := backtickRun(line, i)
				if end := matchingBacktickRun(line, i+run, run); end >= 0 {
					i = end + run
					continue
				}
				i += run
				continue
			}
			if line[i] != '@' || escapedAt(line, i) || emailLocalPart(line, i) {
				i++
				continue
			}
			start := i + 1
			end := start
			for end < len(line) {
				value, size := utf8.DecodeRuneInString(line[end:])
				if !roleNameRune(value) {
					break
				}
				end += size
			}
			if end == start || (end < len(line) && line[end] == '@') {
				i++
				continue
			}
			name := line[start:end]
			if collab.ValidRoleName(name) && !seen[name] {
				seen[name] = true
				mentions = append(mentions, name)
			}
			i = end
		}
	}
	return mentions
}

func markdownFence(line string) (byte, bool) {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, false
	}
	marker := line[0]
	count := 0
	for count < len(line) && line[count] == marker {
		count++
	}
	return marker, count >= 3
}

func backtickRun(text string, start int) int {
	end := start
	for end < len(text) && text[end] == '`' {
		end++
	}
	return end - start
}

func matchingBacktickRun(text string, start, run int) int {
	for i := start; i < len(text); {
		if text[i] != '`' {
			i++
			continue
		}
		current := backtickRun(text, i)
		if current == run {
			return i
		}
		i += current
	}
	return -1
}

func escapedAt(text string, index int) bool {
	backslashes := 0
	for i := index - 1; i >= 0 && text[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func emailLocalPart(text string, index int) bool {
	if index == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._%+-", r)
}

func roleNameRune(value rune) bool {
	return unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_' || value == '-'
}

func mergeMentions(groups ...[]string) []string {
	var merged []string
	seen := map[string]bool{}
	for _, group := range groups {
		for _, name := range group {
			if !seen[name] {
				seen[name] = true
				merged = append(merged, name)
			}
		}
	}
	return merged
}

func parseRecordRef(value string) (*collab.RecordRef, error) {
	room, seqText, ok := strings.Cut(value, "#")
	seq, err := strconv.ParseUint(seqText, 10, 64)
	if !ok || strings.TrimSpace(room) == "" || err != nil || seq == 0 {
		return nil, fmt.Errorf("invalid --reply-to %q: use <room>#<positive sequence>", value)
	}
	return &collab.RecordRef{Room: strings.TrimSpace(room), Seq: seq}, nil
}

func countSet(values ...string) int {
	count := 0
	for _, value := range values {
		if value != "" {
			count++
		}
	}
	return count
}

func signature(flagValue string) string {
	value := strings.TrimSpace(flagValue)
	if value == "" {
		value = strings.TrimSpace(os.Getenv("TEAROOM_ACTOR"))
	}
	return value
}
