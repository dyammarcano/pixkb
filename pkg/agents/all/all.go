// Package all blank-imports every provider package so their init() registers
// the providers with pkg/agents. Import it (with _) wherever ProviderByName is
// used so codex/claude/agy are available.
package all

import (
	_ "pixkb/pkg/agents/agy"
	_ "pixkb/pkg/agents/claude"
	_ "pixkb/pkg/agents/codex"
)
