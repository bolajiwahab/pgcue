package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// commandCompleter supplies candidates for a command's first positional argument.
type commandCompleter func(*App) []string

var completionScripts = map[string]string{
	"bash": `# bash completion for pgcue
_pgcue_complete() {
    local IFS=$'\n'
    COMPREPLY=($("${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1}"))
}
complete -F _pgcue_complete pgcue
`,
	"zsh": `#compdef pgcue
_pgcue() {
    local -a candidates
    candidates=("${(@f)$("${words[1]}" __complete "${words[@]:1}")}")
    compadd -- "${candidates[@]}"
}
compdef _pgcue pgcue
`,
	"fish": `function __pgcue_complete
    set -l tokens (commandline -opc)
    set -l current (commandline -ct)
    test (count $current) -gt 0; or set current ''
    command $tokens[1] __complete $tokens[2..-1] "$current"
end
complete -c pgcue -f -a '(__pgcue_complete)'
`,
}

func (app *App) printCompletion(_ context.Context, args, _ []string) error {
	shell := args[0]
	script, ok := completionScripts[shell]
	if !ok {
		return inputError(fmt.Errorf("unsupported shell %q; expected bash, zsh, or fish", shell))
	}
	_, err := io.WriteString(app.Out, script)
	return err
}

func (app *App) printCompletionCandidates(args []string) error {
	candidates := app.completionCandidates(args)
	if len(candidates) == 0 {
		return nil
	}
	_, err := fmt.Fprintln(app.Out, strings.Join(candidates, "\n"))
	return err
}

func (app *App) completionCandidates(args []string) []string {
	if len(args) == 0 {
		args = []string{""}
	}
	current, completed := args[len(args)-1], args[:len(args)-1]
	commands := commandRegistry()
	if len(completed) == 0 {
		candidates := slices.Concat(slices.Sorted(maps.Keys(commands)), connectionFlags)
		return prefixed(append(candidates, helpFlag, shortHelpFlag, versionFlag), current, "")
	}
	command, ok := commands[completed[0]]
	rest := completed[1:]
	if strings.HasPrefix(completed[0], "-") {
		// The interactive browser takes connection options only.
		command, ok, rest = builtInCommand{options: fixedOptions(connectionFlags)}, true, completed
	}
	if !ok {
		return nil
	}

	var flags []string
	if command.options != nil {
		flags = command.options(app, rest)
	}
	positionals, pendingFlag, ok := parseCompletionArgs(rest, flags, max(command.maxArgs, 0))
	if !ok {
		return nil
	}
	if pendingFlag != "" {
		if pendingFlag == outputFlag {
			return prefixed(outputFormats, current, "")
		}
		return nil
	}
	if value, ok := strings.CutPrefix(current, outputFlag+"="); ok && slices.Contains(flags, outputFlag) {
		return prefixed(outputFormats, value, outputFlag+"=")
	}
	if positionals == 0 && command.complete != nil {
		return prefixed(command.complete(app), current, "")
	}
	return prefixed(flags, current, "")
}

// parseCompletionArgs counts positional arguments and identifies an option awaiting a value.
func parseCompletionArgs(completed, flags []string, maxPositionals int) (positionals int, pendingFlag string, ok bool) {
	for i := 0; i < len(completed); i++ {
		if slices.Contains(switchFlags, completed[i]) {
			continue
		}
		if !strings.HasPrefix(completed[i], "--") {
			positionals++
			continue
		}
		pendingFlag, ok = consumeCompletionOption(completed, &i, flags)
		if !ok {
			return 0, "", false
		}
	}

	if positionals > maxPositionals {
		return 0, "", false
	}
	return positionals, pendingFlag, true
}

// consumeCompletionOption allows a final option to await its value; other options must be complete.
func consumeCompletionOption(completed []string, index *int, flags []string) (pendingFlag string, ok bool) {
	name, _, inline := strings.Cut(completed[*index], "=")
	if !slices.Contains(flags, name) {
		return "", false
	}
	if !inline && *index == len(completed)-1 {
		return name, true
	}
	_, _, err := optionValue(completed, index)
	return "", err == nil
}

func completeShell(_ *App) []string {
	return slices.Sorted(maps.Keys(completionScripts))
}

func completeCheck(app *App) []string {
	return app.catalogue.CheckIDs()
}

func completeTopic(app *App) []string {
	return app.catalogue.TopicIDs()
}

func completeRunnable(app *App) []string {
	return slices.Concat(app.catalogue.CheckIDs(), app.catalogue.InvestigationIDs())
}

func completeInvestigation(app *App) []string {
	return app.catalogue.InvestigationIDs()
}

func completeDescribe(app *App) []string {
	return documentationIDs(app)
}

func prefixed(values []string, prefix, replacement string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			unique[replacement+value] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(unique))
}
