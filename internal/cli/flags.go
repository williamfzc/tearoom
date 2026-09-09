// flags.go owns argument parsing for every cli command: the shared flag-set
// constructor, flags in any order with -- as a literal boundary, and the
// repeatable string flag. Command behavior lives in cli.go.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

func newFlags(name string, out io.Writer, help string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.Usage = func() { fmt.Fprint(out, help) }
	return flags
}

// parse permits flags in any order while preserving -- as a literal boundary.
func parse(flags *flag.FlagSet, args []string) error {
	var options, positionals []string
	literal := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if literal {
			positionals = append(positionals, arg)
			continue
		}
		if arg == "--" {
			literal = true
			continue
		}
		if isHelp(arg) {
			flags.Usage()
			return flag.ErrHelp
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		defined := flags.Lookup(name)
		if defined == nil {
			return fmt.Errorf("flag provided but not defined: --%s", name)
		}
		options = append(options, arg)
		if strings.Contains(arg, "=") || isBoolFlag(defined) {
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("flag needs an argument: --%s", name)
		}
		i++
		options = append(options, args[i])
	}
	parsed := options
	if literal {
		parsed = append(parsed, "--")
	}
	return flags.Parse(append(parsed, positionals...))
}

func parseNoArgs(flags *flag.FlagSet, args []string) error {
	if err := parse(flags, args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return nil
}

func isBoolFlag(value *flag.Flag) bool {
	type boolFlag interface{ IsBoolFlag() bool }
	flagValue, ok := value.Value.(boolFlag)
	return ok && flagValue.IsBoolFlag()
}

// stringList collects a repeatable flag value.
type stringList struct {
	values []string
}

func (s *stringList) String() string { return strings.Join(s.values, ",") }

func (s *stringList) Set(value string) error {
	s.values = append(s.values, value)
	return nil
}
