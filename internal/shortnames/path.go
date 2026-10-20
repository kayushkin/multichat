package shortnames

import (
	"fmt"
	"regexp"
	"strings"
)

// The contract's path format: one to three segments joined by "/", each
// lowercase ASCII letters and digits in words joined by single hyphens, 1–40
// characters.
const (
	maximumSegmentLength = 40
	maximumSegmentCount  = 3
)

var segmentPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// checkSegment says why segment is not a valid path segment, or "" when it is.
func checkSegment(segment string) string {
	switch {
	case segment == "":
		return "a segment is empty"
	case len(segment) > maximumSegmentLength:
		return fmt.Sprintf("segment %q is longer than %d characters", segment, maximumSegmentLength)
	case !segmentPattern.MatchString(segment):
		return fmt.Sprintf("segment %q is not lowercase ascii letters and digits in words joined by single hyphens", segment)
	}
	return ""
}

// checkPath says why path is not a valid short-name path, or "" when it is.
func checkPath(path string) string {
	segments := strings.Split(path, "/")
	if len(segments) > maximumSegmentCount {
		return fmt.Sprintf("%d segments; at most %d", len(segments), maximumSegmentCount)
	}
	for _, segment := range segments {
		if why := checkSegment(segment); why != "" {
			return why
		}
	}
	return ""
}

func firstSegment(path string) string {
	first, _, _ := strings.Cut(path, "/")
	return first
}
