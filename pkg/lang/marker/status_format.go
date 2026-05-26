package marker

import (
	"regexp"
	"strings"
)

func IsDone(body string) bool {
	if _, fields, ok := extractATMBlock(body); ok && isTerminalStatus(fields["status"]) {
		return true
	}
	return doneSuffix.MatchString(strings.TrimSpace(body))
}

func IsSkipped(body string) bool {
	if _, fields, ok := extractATMBlock(body); ok && strings.EqualFold(fields["status"], "skipped") {
		return true
	}
	return false
}

func FormatBlockBody(body string) string {
	body, tag, hasTag := extractTrailingTag(body)
	if hasTag {
		return appendTagLine(body, tag)
	}
	return body
}

func extractTrailingTag(body string) (string, string, bool) {
	if _, _, ok := extractATMBlock(body); ok {
		return body, "", false
	}
	if clean, tag, ok := extractTag(body, doneMarker, doneSuffix); ok {
		return clean, tag, true
	}
	if clean, tag, ok := extractTag(body, runningLineMarker, runningSuffix); ok {
		return clean, tag, true
	}
	return body, "", false
}

func extractTag(body string, markerRe, suffixRe *regexp.Regexp) (string, string, bool) {
	core, eol := splitTrailingLineEnding(body)

	lineStart := strings.LastIndexAny(core, "\r\n") + 1
	lastLine := core[lineStart:]
	if markerRe.MatchString(strings.TrimSpace(lastLine)) {
		prefix := strings.TrimRight(core[:lineStart], "\r\n")
		if prefix == "" {
			return eol, strings.TrimSpace(lastLine), true
		}
		return prefix + eol, strings.TrimSpace(lastLine), true
	}

	tag := suffixRe.FindString(core)
	if tag == "" {
		return body, "", false
	}
	return suffixRe.ReplaceAllString(core, "") + eol, strings.TrimSpace(tag), true
}
