package usecase

import "strings"

// MaxShared is how many shared interests or skills a recommendation shows (AI-2.2).
const MaxShared = 3

// SharedTags returns up to MaxShared tags both users have: first the research interests
// both have, then the skills both have, each in the viewer's order and spelling. Names
// match when they are equal ignoring letter case and spaces around them; a name is listed
// once even when it is both an interest and a skill. The result may be empty, never nil.
func SharedTags(myInterests, mySkills, theirInterests, theirSkills []string) []string {
	out := make([]string, 0, MaxShared)
	seen := make(map[string]bool, MaxShared)
	add := func(mine, theirs []string) {
		have := make(map[string]bool, len(theirs))
		for _, t := range theirs {
			have[tagKey(t)] = true
		}
		for _, t := range mine {
			k := tagKey(t)
			if len(out) == MaxShared {
				return
			}
			if k == "" || !have[k] || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, strings.TrimSpace(t))
		}
	}
	add(myInterests, theirInterests)
	add(mySkills, theirSkills)
	return out
}

func tagKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
