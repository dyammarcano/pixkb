package okf

import (
	"regexp"
	"strings"
)

// mdLinkRe captures the target of a markdown inline link: [text](target).
var mdLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

// ParseLinks resolves [..](path.md) markdown links to bundle-relative IDs.
// It ignores absolute (http/https) links, non-.md targets, strips a leading
// "./", drops "#anchor"/"?query" suffixes, and de-duplicates while preserving
// first-seen order. The result is always non-nil.
func ParseLinks(body string) []string {
	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, m := range mdLinkRe.FindAllStringSubmatch(body, -1) {
		target := strings.TrimSpace(m[1])
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			target = target[:i]
		}
		if target == "" {
			continue
		}
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(target), ".md") {
			continue
		}
		target = normalizeRelPath(target)
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
}

// normalizeRelPath converts a relative markdown path to a clean
// forward-slash bundle-relative ID, collapsing "./" and "../" segments.
func normalizeRelPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	parts := strings.Split(p, "/")
	stack := make([]string, 0, len(parts))
	for _, seg := range parts {
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			stack = append(stack, seg)
		}
	}
	return strings.Join(stack, "/")
}
