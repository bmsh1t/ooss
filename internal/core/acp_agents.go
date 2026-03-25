package core

import (
	"slices"
	"strings"
)

// ACPAgentDef describes a built-in ACP agent command.
type ACPAgentDef struct {
	Command string
	Args    []string
}

var builtinACPAgents = map[string]ACPAgentDef{
	"claude-code": {Command: "npx", Args: []string{"-y", "@zed-industries/claude-code-acp@latest"}},
	"codex":       {Command: "npx", Args: []string{"-y", "@zed-industries/codex-acp"}},
	"opencode":    {Command: "opencode", Args: []string{"acp"}},
	"gemini":      {Command: "gemini", Args: []string{"--experimental-acp"}},
}

// ResolveBuiltinACPAgent returns the built-in ACP agent definition if present.
func ResolveBuiltinACPAgent(name string) (ACPAgentDef, bool) {
	def, ok := builtinACPAgents[name]
	return def, ok
}

// IsBuiltinACPAgent returns true if the given name matches a built-in ACP agent.
func IsBuiltinACPAgent(name string) bool {
	_, ok := builtinACPAgents[name]
	return ok
}

// BuiltinACPAgentNames returns a sorted list of built-in ACP agent names.
func BuiltinACPAgentNames() []string {
	names := make([]string, 0, len(builtinACPAgents))
	for name := range builtinACPAgents {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// BuiltinACPAgentNamesString returns a comma-separated list of built-in ACP agent names.
func BuiltinACPAgentNamesString() string {
	return strings.Join(BuiltinACPAgentNames(), ", ")
}
