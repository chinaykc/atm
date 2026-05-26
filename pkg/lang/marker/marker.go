package marker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/ir"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DoneInfo struct {
	Start    time.Time
	End      time.Time
	Runs     int
	ID       string
	Source   string
	Rendered string
	Report   string
	Messages []ir.OutputMessage
}

type FailedInfo struct {
	Start    time.Time
	End      time.Time
	Runs     int
	Error    string
	ID       string
	Source   string
	Rendered string
	Report   string
	Messages []ir.OutputMessage
}

type SkippedInfo struct {
	Time     time.Time
	Reason   string
	ID       string
	Source   string
	Rendered string
	Report   string
}

type RunningInfo struct {
	Active    bool
	Start     time.Time
	StepIndex int
	StepRuns  int
	TotalRuns int
	ID        string
	Source    string
	Rendered  string
	Report    string
	Messages  []ir.OutputMessage
}

var doneMarker = regexp.MustCompile(`^\[done(?:\|[^\]]*)?\]$`)
var doneSuffix = regexp.MustCompile(`[ \t]*\[done(?:\|[^\]]*)?\]$`)
var runningLineMarker = regexp.MustCompile(`^\[running\|[^\]]+\]$`)
var runningMarker = regexp.MustCompile(`^\[running\|([0-9]{8}-[0-9]{2}:[0-9]{2})\|step=([0-9]+)\|step-runs=([0-9]+)x\|total=([0-9]+)x\]$`)
var legacyRunningMarker = regexp.MustCompile(`^\[running\|([0-9]{8}-[0-9]{2}:[0-9]{2})\|([0-9]+)x\]$`)
var runningSuffix = regexp.MustCompile(`[ \t]*\[running\|[^\]]+\]$`)
var atmQuoteLine = regexp.MustCompile(`^[ \t]*> ?`)

func AppendDone(body string, info DoneInfo) string {
	if IsDone(body) {
		return body
	}

	body, _ = RemoveRunning(body)
	body, _ = RemoveDone(body)
	return appendTagLine(body, formatDoneMarker(info))
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := make([]string, 0)
	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}
		lines = append(lines, content[start:i+1])
		start = i + 1
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

func firstField(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func AppendFailed(body string, info FailedInfo) string {
	if IsDone(body) {
		return body
	}

	body, _ = RemoveRunning(body)
	body, _ = RemoveDone(body)
	return appendTagLine(body, formatFailedMarker(info))
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
	id, source, report := normalizeReportIdentity(info.ID, info.Source, info.Report)
	rendered := strings.TrimSpace(info.Rendered)
	status := "done"
	lines := []string{
		"[!ATM]",
		"status: " + status,
		"started: " + formatDisplayTime(info.Start),
		"finished: " + formatDisplayTime(info.End),
		"duration: " + formatDoneDuration(info.End.Sub(info.Start)),
		fmt.Sprintf("runs: %dx", info.Runs),
	}
	lines = appendReportIdentity(lines, id, source, rendered, report)
	lines = appendMessageLines(lines, info.Messages)
	return reportEnvelope(id, source, rendered, report, status, quoteLines(lines))
}

func formatSkippedMarker(info SkippedInfo) string {
	id, source, report := normalizeReportIdentity(info.ID, info.Source, info.Report)
	rendered := strings.TrimSpace(info.Rendered)
	status := "skipped"
	if info.Time.IsZero() {
		info.Time = time.Now()
	}
	reason := strings.TrimSpace(info.Reason)
	if reason == "" {
		reason = "condition evaluated false"
	}
	lines := []string{
		"[!ATM]",
		"status: " + status,
		"time: " + formatDisplayTime(info.Time),
		"reason: " + reason,
	}
	lines = appendReportIdentity(lines, id, source, rendered, report)
	return reportEnvelope(id, source, rendered, report, status, quoteLines(lines))
}

func formatFailedMarker(info FailedInfo) string {
	id, source, report := normalizeReportIdentity(info.ID, info.Source, info.Report)
	rendered := strings.TrimSpace(info.Rendered)
	status := "failed"
	reason := strings.TrimSpace(info.Error)
	if reason == "" {
		reason = "task failed"
	}
	reason = strings.Join(strings.Fields(reason), " ")
	lines := []string{
		"[!ATM]",
		"status: " + status,
		"started: " + formatDisplayTime(info.Start),
		"finished: " + formatDisplayTime(info.End),
		"duration: " + formatDoneDuration(info.End.Sub(info.Start)),
		fmt.Sprintf("runs: %dx", info.Runs),
		"error: " + reason,
	}
	lines = appendReportIdentity(lines, id, source, rendered, report)
	lines = appendMessageLines(lines, info.Messages)
	return reportEnvelope(id, source, rendered, report, status, quoteLines(lines))
}

func formatRunningMarker(info RunningInfo) string {
	id, source, report := normalizeReportIdentity(info.ID, info.Source, info.Report)
	rendered := strings.TrimSpace(info.Rendered)
	status := "running"
	lines := []string{
		"[!ATM]",
		"status: " + status,
		"started: " + formatDisplayTime(info.Start),
		fmt.Sprintf("step: %d", info.StepIndex+1),
		fmt.Sprintf("step-runs: %dx", info.StepRuns),
		fmt.Sprintf("total-runs: %dx", info.TotalRuns),
	}
	lines = appendReportIdentity(lines, id, source, rendered, report)
	lines = appendMessageLines(lines, info.Messages)
	return reportEnvelope(id, source, rendered, report, status, quoteLines(lines))
}

func reportEnvelope(id, source, rendered, report, status, visible string) string {
	var attrs []string
	attrs = append(attrs, "v=2")
	if id != "" {
		attrs = append(attrs, "id="+id)
	}
	if source != "" {
		attrs = append(attrs, "source="+source)
	}
	if rendered != "" {
		attrs = append(attrs, "rendered="+rendered)
	}
	if report != "" {
		attrs = append(attrs, "report="+report)
	}
	if status != "" {
		attrs = append(attrs, "status="+status)
	}
	return "<!-- atm:report " + strings.Join(attrs, " ") + " -->\n" + visible + "\n<!-- /atm:report -->"
}

func appendReportIdentity(lines []string, id, source, rendered, report string) []string {
	if id != "" {
		lines = append(lines, "id: "+id)
	}
	if source != "" {
		lines = append(lines, "source: "+source)
	}
	if rendered != "" {
		lines = append(lines, "rendered: "+rendered)
	}
	if report != "" {
		lines = append(lines, "report: "+report)
	}
	return lines
}

func normalizeReportIdentity(id, source, report string) (string, string, string) {
	id = strings.TrimSpace(id)
	source = strings.TrimSpace(source)
	report = strings.TrimSpace(report)
	if report == "" && id != "" {
		report = ATMReportPath(id)
	}
	return id, source, report
}

func ATMReportPath(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return ".atm/reports/" + id + ".md"
}

func formatDisplayTime(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

func isTerminalStatus(status string) bool {
	return strings.EqualFold(status, "done") || strings.EqualFold(status, "skipped") || strings.EqualFold(status, "failed")
}

func appendMessageLines(lines []string, messages []ir.OutputMessage) []string {
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

func VisibleATMReport(body string) string {
	if _, _, visible, ok := extractATMReportEnvelope(body); ok {
		return visible
	}

	core, _ := splitTrailingLineEnding(body)
	lines := splitLines(core)
	if len(lines) == 0 {
		return ""
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
		return ""
	}

	atmStart := -1
	for i := len(lines) - 1; i >= start; i-- {
		if strings.TrimSpace(unquoteATMLine(lines[i])) == "[!ATM]" {
			atmStart = i
			break
		}
	}
	if atmStart < 0 {
		return ""
	}
	return strings.TrimRight(strings.Join(lines[atmStart:], ""), "\r\n")
}

func ATMReportID(body string) (string, bool) {
	_, fields, ok := extractATMBlock(body)
	id := strings.TrimSpace(fields["id"])
	return id, ok && id != ""
}

type ATMReportMeta struct {
	ID       string
	Status   string
	Source   string
	Rendered string
	Report   string
}

func ATMReportMetadata(body string) (ATMReportMeta, bool) {
	_, fields, ok := extractATMBlock(body)
	if !ok {
		return ATMReportMeta{}, false
	}
	meta := ATMReportMeta{
		ID:       strings.TrimSpace(fields["id"]),
		Status:   strings.TrimSpace(fields["status"]),
		Source:   strings.TrimSpace(fields["source"]),
		Rendered: strings.TrimSpace(fields["rendered"]),
		Report:   strings.TrimSpace(fields["report"]),
	}
	return meta, meta.ID != ""
}

func RewriteATMReportIdentity(body, id, source, report string) (string, bool) {
	clean, fields, visible, ok := extractATMReportEnvelope(body)
	if !ok {
		return body, false
	}
	status := strings.TrimSpace(fields["status"])
	rendered := strings.TrimSpace(fields["rendered"])
	visible = rewriteVisibleATMReportIdentity(visible, id, source, report)
	return clean + reportEnvelope(id, source, rendered, report, status, visible), true
}

func rewriteVisibleATMReportIdentity(visible, id, source, report string) string {
	lines := splitLines(visible)
	if len(lines) == 0 {
		return visible
	}
	insert := len(lines)
	var out []string
	for _, line := range lines {
		text := strings.TrimSpace(unquoteATMLine(line))
		if strings.HasPrefix(text, "id:") || strings.HasPrefix(text, "source:") || strings.HasPrefix(text, "report:") {
			continue
		}
		if insert == len(lines) && (text == "" || strings.HasPrefix(text, "messages:")) {
			insert = len(out)
		}
		out = append(out, line)
	}
	if insert > len(out) {
		insert = len(out)
	}
	identity := []string{}
	if strings.TrimSpace(id) != "" {
		identity = append(identity, "> id: "+strings.TrimSpace(id)+"\n")
	}
	if strings.TrimSpace(source) != "" {
		identity = append(identity, "> source: "+strings.TrimSpace(source)+"\n")
	}
	if strings.TrimSpace(report) != "" {
		identity = append(identity, "> report: "+strings.TrimSpace(report)+"\n")
	}
	out = append(out[:insert], append(identity, out[insert:]...)...)
	return strings.TrimRight(strings.Join(out, ""), "\r\n")
}

func ReportIdentityForSource(body, context string) (string, string) {
	clean := reportSourceBody(body)
	sourceClean := clean
	context = strings.TrimSpace(context)
	if context != "" {
		sourceClean = context + "\n\n" + sourceClean
	}
	sum := sha256.Sum256([]byte(sourceClean))
	hash := hex.EncodeToString(sum[:])
	short := hash
	if len(short) > 10 {
		short = short[:10]
	}
	slug := reportSlug(clean)
	if slug == "" {
		slug = "task"
	}
	return slug + "-" + short, "sha256:" + hash
}

func SourcePromptHash(body, context string) string {
	clean := reportSourceBody(body)
	context = strings.TrimSpace(context)
	if context != "" {
		clean = strings.TrimSpace(context) + "\n\n" + clean
	}
	sum := sha256.Sum256([]byte(clean))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func reportSourceBody(body string) string {
	if clean, _, ok := extractATMBlock(body); ok {
		body = clean
	}
	if clean, _, err := StripRunning(body); err == nil {
		body = clean
	}
	if clean, ok := RemoveDone(body); ok {
		body = clean
	}
	return strings.TrimSpace(body)
}

func reportSlug(body string) string {
	lines := splitLines(body)
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		if firstField(text) == "/task" {
			continue
		}
		if strings.HasPrefix(text, "/") {
			text = strings.TrimPrefix(firstField(text), "/")
		}
		return slugText(text)
	}
	return ""
}

func slugText(text string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 48 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
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
	if clean, fields, _, ok := extractATMReportEnvelope(body); ok {
		return clean, fields, true
	}

	core, eol := splitTrailingLineEnding(body)
	lines := splitLines(core)
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

	fields := parseATMQuoteFields(lines[atmStart+1:])

	clean := strings.Join(lines[:atmStart], "")
	clean = strings.TrimRight(clean, "\r\n")
	if clean == "" {
		return eol, fields, true
	}
	return clean + eol, fields, true
}

func extractATMReportEnvelope(body string) (string, map[string]string, string, bool) {
	core, eol := splitTrailingLineEnding(body)
	lines := splitLines(core)
	if len(lines) == 0 {
		return body, nil, "", false
	}
	end := len(lines) - 1
	if strings.TrimSpace(lines[end]) != "<!-- /atm:report -->" {
		return body, nil, "", false
	}

	start := -1
	for i := end - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "<!-- atm:report") && strings.HasSuffix(trimmed, "-->") {
			start = i
			break
		}
	}
	if start < 0 {
		return body, nil, "", false
	}

	fields := parseATMReportAttrs(lines[start])
	inner := lines[start+1 : end]
	atmStart := -1
	for i := len(inner) - 1; i >= 0; i-- {
		if strings.TrimSpace(unquoteATMLine(inner[i])) == "[!ATM]" {
			atmStart = i
			break
		}
	}
	if atmStart >= 0 {
		for key, value := range parseATMQuoteFields(inner[atmStart+1:]) {
			fields[key] = value
		}
	}

	visible := strings.TrimRight(strings.Join(inner, ""), "\r\n")
	clean := strings.Join(lines[:start], "")
	clean = strings.TrimRight(clean, "\r\n")
	if clean == "" {
		return eol, fields, visible, true
	}
	return clean + eol, fields, visible, true
}

func parseATMReportAttrs(line string) map[string]string {
	fields := make(map[string]string)
	text := strings.TrimSpace(line)
	text = strings.TrimPrefix(text, "<!--")
	text = strings.TrimSuffix(text, "-->")
	text = strings.TrimSpace(text)
	parts := strings.Fields(text)
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		fields[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return fields
}

func parseATMQuoteFields(lines []string) map[string]string {
	fields := make(map[string]string)
	for _, line := range lines {
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
	return fields
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
	return RunningInfo{
		Active:    true,
		Start:     start,
		StepIndex: stepIndex - 1,
		StepRuns:  stepRuns,
		TotalRuns: totalRuns,
		ID:        strings.TrimSpace(fields["id"]),
		Source:    strings.TrimSpace(fields["source"]),
		Rendered:  strings.TrimSpace(fields["rendered"]),
		Report:    strings.TrimSpace(fields["report"]),
	}, nil
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
