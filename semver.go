package flagstack

import (
	"regexp"
	"strings"
)

var semverPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)(?:\.(0|[1-9][0-9]*))?(?:\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?)?$`)

type parsedSemver struct {
	major      string
	minor      string
	patch      string
	prerelease []string
}

func compareSemver(left, right string) (int, bool) {
	parsedLeft, ok := parseSemver(left)
	if !ok {
		return 0, false
	}
	parsedRight, ok := parseSemver(right)
	if !ok {
		return 0, false
	}
	for _, pair := range [][2]string{{parsedLeft.major, parsedRight.major}, {parsedLeft.minor, parsedRight.minor}, {parsedLeft.patch, parsedRight.patch}} {
		if cmp := compareIntegerStrings(pair[0], pair[1]); cmp != 0 {
			return cmp, true
		}
	}
	return comparePrerelease(parsedLeft.prerelease, parsedRight.prerelease), true
}

func parseSemver(input string) (parsedSemver, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "v") {
		trimmed = "v" + trimmed
	}
	match := semverPattern.FindStringSubmatch(trimmed)
	if match == nil {
		return parsedSemver{}, false
	}
	prerelease := []string{}
	if match[4] != "" {
		prerelease = strings.Split(match[4], ".")
		for _, identifier := range prerelease {
			if numeric(identifier) && len(identifier) > 1 && identifier[0] == '0' {
				return parsedSemver{}, false
			}
		}
	}
	minor := match[2]
	if minor == "" {
		minor = "0"
	}
	patch := match[3]
	if patch == "" {
		patch = "0"
	}
	return parsedSemver{major: match[1], minor: minor, patch: patch, prerelease: prerelease}, true
}

func compareIntegerStrings(left, right string) int {
	if left == right {
		return 0
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	if left < right {
		return -1
	}
	return 1
}

func comparePrerelease(left, right []string) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}
	limit := min(len(left), len(right))
	for i := 0; i < limit; i++ {
		if left[i] == right[i] {
			continue
		}
		ln, rn := numeric(left[i]), numeric(right[i])
		if ln != rn {
			if ln {
				return -1
			}
			return 1
		}
		if ln {
			return compareIntegerStrings(left[i], right[i])
		}
		if left[i] < right[i] {
			return -1
		}
		return 1
	}
	if len(left) == len(right) {
		return 0
	}
	if len(left) < len(right) {
		return -1
	}
	return 1
}

func numeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
