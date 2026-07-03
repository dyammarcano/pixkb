// Package okf re-exports the canonical OKF concept type for external reuse.
package okf

import internalokf "pixkb/internal/okf"

// Concept is a type alias for internal/okf.Concept so external consumers can
// depend on the stable pkg/ path while sharing the exact same type used
// internally (no conversion needed at the boundary).
type Concept = internalokf.Concept
