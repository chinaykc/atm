package dsl

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func AppendDone(body string, info DoneInfo) string {
	if IsDone(body) {
		return body
	}

	body, _ = RemoveRunning(body)
	body, _ = RemoveDone(body)
	return appendTagLine(body, formatDoneMarker(info))
}

func AppendSkipped(body string, info SkippedInfo) string {
	if IsDone(body) {
		return body
	}

	body, _ = RemoveRunning(body)
	body, _ = RemoveDone(body)
	return appendTagLine(body, formatSkippedMarker(info))
}

func AppendRunning(body string, info RunningInfo) string {
	if hasRunning(body) {
		return replaceRunning(body, info)
	}

	return appendTagLine(body, formatRunningMarker(info))
}

func appendTagLine(body, marker string) string {
	for _, eol := range []string{"\r\n", "\n", "\r"} {
		if strings.HasSuffix(body, eol) {
			return body + marker + eol
		}
	}
	return body + "\n" + marker
}

func replaceRunning(body string, info RunningInfo) string {
	clean, _ := RemoveRunning(body)
	return AppendRunning(clean, info)
}

func RemoveDone(body string) (string, bool) {
	if clean, fields, ok := extractATMBlock(body); ok && isTerminalStatus(fields["status"]) {
		return clean, true
	}
	return removeTag(body, doneMarker, doneSuffix)
}

func StripRunning(body string) (string, RunningInfo, error) {
	if clean, fields, ok := extractATMBlock(body); ok {
		if strings.EqualFold(fields["status"], "running") {
			info, err := parseATMRunning(fields)
			if err != nil {
				return "", RunningInfo{}, err
			}
			return clean, info, nil
		}
	}

	line, ok := lastTagLine(body, runningSuffix)
	if !ok {
		return body, RunningInfo{}, nil
	}

	markerText := strings.TrimSpace(line)
	if !strings.HasPrefix(markerText, "[running|") {
		markerText = strings.TrimSpace(runningSuffix.FindString(markerText))
	}
	matches := runningMarker.FindStringSubmatch(markerText)
	legacyMatches := legacyRunningMarker.FindStringSubmatch(markerText)
	if matches == nil && legacyMatches == nil {
		return body, RunningInfo{}, nil
	}
	timePart := ""
	stepIndex := 0
	stepRuns := 0
	totalRuns := 0
	if matches != nil {
		timePart = matches[1]
		parsedStep, err := strconv.Atoi(matches[2])
		if err != nil {
			return "", RunningInfo{}, fmt.Errorf("invalid running step %q", matches[2])
		}
		if parsedStep > 0 {
			stepIndex = parsedStep - 1
		}
		var parseErr error
		stepRuns, parseErr = strconv.Atoi(matches[3])
		if parseErr != nil {
			return "", RunningInfo{}, fmt.Errorf("invalid running step run count %q", matches[3])
		}
		totalRuns, parseErr = strconv.Atoi(matches[4])
		if parseErr != nil {
			return "", RunningInfo{}, fmt.Errorf("invalid running total run count %q", matches[4])
		}
	} else {
		timePart = legacyMatches[1]
		var err error
		stepRuns, err = strconv.Atoi(legacyMatches[2])
		if err != nil {
			return "", RunningInfo{}, fmt.Errorf("invalid running run count %q", legacyMatches[2])
		}
		totalRuns = stepRuns
	}

	start, err := time.ParseInLocation("20060102-15:04", timePart, time.Local)
	if err != nil {
		return "", RunningInfo{}, fmt.Errorf("invalid running start time %q", timePart)
	}
	clean, _ := RemoveRunning(body)
	return clean, RunningInfo{Active: true, Start: start, StepIndex: stepIndex, StepRuns: stepRuns, TotalRuns: totalRuns}, nil
}

func RemoveRunning(body string) (string, bool) {
	if clean, fields, ok := extractATMBlock(body); ok && strings.EqualFold(fields["status"], "running") {
		return clean, true
	}
	return removeTag(body, runningLineMarker, runningSuffix)
}

func hasRunning(body string) bool {
	if _, fields, ok := extractATMBlock(body); ok && strings.EqualFold(fields["status"], "running") {
		return true
	}
	_, ok := lastTagLine(body, runningSuffix)
	return ok
}

func removeTag(body string, markerRe, suffixRe *regexp.Regexp) (string, bool) {
	core, eol := splitTrailingLineEnding(body)

	lineStart := strings.LastIndexAny(core, "\r\n") + 1
	lastLine := core[lineStart:]
	if markerRe.MatchString(strings.TrimSpace(lastLine)) {
		prefix := strings.TrimRight(core[:lineStart], "\r\n")
		if prefix == "" {
			return eol, true
		}
		return prefix + eol, true
	}
	if suffixRe.MatchString(strings.TrimSpace(core)) {
		return suffixRe.ReplaceAllString(core, "") + eol, true
	}
	return body, false
}

func lastTagLine(body string, suffixRe *regexp.Regexp) (string, bool) {
	core, _ := splitTrailingLineEnding(body)
	if !suffixRe.MatchString(strings.TrimSpace(core)) {
		return "", false
	}
	lineStart := strings.LastIndexAny(core, "\r\n") + 1
	return core[lineStart:], true
}

func splitTrailingLineEnding(s string) (core, eol string) {
	for _, candidate := range []string{"\r\n", "\n", "\r"} {
		if strings.HasSuffix(s, candidate) {
			return strings.TrimSuffix(s, candidate), candidate
		}
	}
	return s, ""
}

func formatDoneMarker(info DoneInfo) string {
	lines := []string{
		"[!ATM]",
		"status: done",
		"started: " + formatDisplayTime(info.Start),
		"finished: " + formatDisplayTime(info.End),
		"duration: " + formatDoneDuration(info.End.Sub(info.Start)),
		fmt.Sprintf("runs: %dx", info.Runs),
	}
	lines = appendMessageLines(lines, info.Messages)
	return quoteLines(lines)
}

func formatSkippedMarker(info SkippedInfo) string {
	if info.Time.IsZero() {
		info.Time = time.Now()
	}
	reason := strings.TrimSpace(info.Reason)
	if reason == "" {
		reason = "condition evaluated false"
	}
	lines := []string{
		"[!ATM]",
		"status: skipped",
		"time: " + formatDisplayTime(info.Time),
		"reason: " + reason,
	}
	return quoteLines(lines)
}

func formatRunningMarker(info RunningInfo) string {
	lines := []string{
		"[!ATM]",
		"status: running",
		"started: " + formatDisplayTime(info.Start),
		fmt.Sprintf("step: %d", info.StepIndex+1),
		fmt.Sprintf("step-runs: %dx", info.StepRuns),
		fmt.Sprintf("total-runs: %dx", info.TotalRuns),
	}
	lines = appendMessageLines(lines, info.Messages)
	return quoteLines(lines)
}

func formatDoneTime(t time.Time) string {
	return t.Format("20060102-15:04")
}

func formatDisplayTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

func isTerminalStatus(status string) bool {
	return strings.EqualFold(status, "done") || strings.EqualFold(status, "skipped")
}

func appendMessageLines(lines []string, messages []OutputMessage) []string {
	if len(messages) == 0 {
		return lines
	}
	lines = append(lines, "", "messages:")
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "assistant"
		}
		tool := strings.TrimSpace(message.Tool)
		label := role
		if tool != "" {
			label += " (" + tool + ")"
		}
		if agent := strings.TrimSpace(message.Agent); agent != "" {
			label += " [" + agent + "]"
		}
		lines = append(lines, "- "+label+":")
		text := strings.TrimRight(message.Text, "\r\n")
		if text == "" {
			lines = append(lines, "  (empty)")
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			lines = append(lines, "  "+strings.TrimRight(line, "\r"))
		}
	}
	return lines
}

func quoteLines(lines []string) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if line == "" {
			b.WriteString(">")
			continue
		}
		b.WriteString("> ")
		b.WriteString(line)
	}
	return b.String()
}

func extractATMBlock(body string) (string, map[string]string, bool) {
	core, eol := splitTrailingLineEnding(body)
	lines := SplitLines(core)
	if len(lines) == 0 {
		return body, nil, false
	}

	start := len(lines)
	for start > 0 {
		line := strings.TrimRight(lines[start-1], "\r\n")
		if !atmQuoteLine.MatchString(line) {
			break
		}
		start--
	}
	if start == len(lines) {
		return body, nil, false
	}

	atmStart := -1
	for i := len(lines) - 1; i >= start; i-- {
		if strings.TrimSpace(unquoteATMLine(lines[i])) == "[!ATM]" {
			atmStart = i
			break
		}
	}
	if atmStart < 0 {
		return body, nil, false
	}

	fields := make(map[string]string)
	for _, line := range lines[atmStart+1:] {
		unquoted := strings.TrimSpace(unquoteATMLine(line))
		if unquoted == "" || strings.HasPrefix(unquoted, "- ") || strings.HasPrefix(unquoted, "messages:") {
			break
		}
		key, value, ok := strings.Cut(unquoted, ":")
		if !ok {
			continue
		}
		fields[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}

	clean := strings.Join(lines[:atmStart], "")
	clean = strings.TrimRight(clean, "\r\n")
	if clean == "" {
		return eol, fields, true
	}
	return clean + eol, fields, true
}

func unquoteATMLine(line string) string {
	line = strings.TrimRight(line, "\r\n")
	return atmQuoteLine.ReplaceAllString(line, "")
}

func parseATMRunning(fields map[string]string) (RunningInfo, error) {
	start, err := parseMarkerTime(fields["started"])
	if err != nil {
		return RunningInfo{}, fmt.Errorf("invalid running start time %q", fields["started"])
	}
	stepIndex, err := parsePositiveField(fields["step"], "running step")
	if err != nil {
		return RunningInfo{}, err
	}
	stepRuns, err := parseRunCountField(fields["step-runs"], "running step run count")
	if err != nil {
		return RunningInfo{}, err
	}
	totalRuns, err := parseRunCountField(fields["total-runs"], "running total run count")
	if err != nil {
		return RunningInfo{}, err
	}
	return RunningInfo{Active: true, Start: start, StepIndex: stepIndex - 1, StepRuns: stepRuns, TotalRuns: totalRuns}, nil
}

func parseMarkerTime(value string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04", "20060102-15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid marker time")
}

func parsePositiveField(value, name string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return parsed, nil
}

func parseRunCountField(value, name string) (int, error) {
	value = strings.TrimSuffix(strings.TrimSpace(value), "x")
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	return parsed, nil
}

func formatDoneDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}

	d = d.Round(time.Second)
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second

	var parts []string
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, "")
}
