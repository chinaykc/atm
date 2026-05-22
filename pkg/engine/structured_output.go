package engine

import (
	"atm/pkg/dsl"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func renderOutputSpec(spec *dsl.OutputSpec, vars map[string]any) (*dsl.OutputSpec, error) {
	if spec == nil {
		return nil, nil
	}
	out := *spec
	var err error
	if out.FileName != "" {
		out.FileName, err = dsl.RenderTemplate(out.FileName, vars)
		if err != nil {
			return nil, fmt.Errorf("output file name template failed: %w", err)
		}
	}
	out.Schema, err = dsl.RenderTemplate(out.Schema, vars)
	if err != nil {
		return nil, fmt.Errorf("output schema template failed: %w", err)
	}
	return &out, nil
}

func renderReturnOutputSpec(spec *dsl.ReturnSpec, vars map[string]any) (*dsl.OutputSpec, error) {
	if spec == nil || spec.Kind != dsl.ReturnStructured || spec.Output == nil {
		return nil, nil
	}
	return renderOutputSpec(spec.Output, vars)
}

func outputTemplateVars(current execContext) map[string]any {
	vars := dsl.CloneVars(current.vars)
	if current.agent != "" {
		vars["agent"] = sanitizeFilePart(current.agent)
		vars["agent_label"] = current.agent
	}
	if current.agentID > 0 {
		vars["agent_index"] = fmt.Sprintf("%d", current.agentID)
	}
	return vars
}

func outputAgentSuffix(current execContext) string {
	if current.agent != "" {
		return sanitizeFilePart(current.agent)
	}
	if current.agentID > 0 {
		return fmt.Sprintf("agent-%d", current.agentID)
	}
	return "agent"
}

func defaultStructuredOutputName(taskIndex, runNumber int, agent string) string {
	stamp := time.Now().Format("20060102150405-000000000")
	name := fmt.Sprintf("task-%03d-run-%03d-output-%s", taskIndex+1, runNumber, stamp)
	if agent != "" {
		name += "-" + sanitizeFilePart(agent)
	}
	return name + ".json"
}

func cleanStructuredOutputName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return ""
	}
	if filepath.Ext(name) == "" {
		name += ".json"
	}
	return name
}

func defaultTextOutputName(taskIndex, runNumber int, agent string) string {
	stamp := time.Now().Format("20060102150405-000000000")
	name := fmt.Sprintf("task-%03d-run-%03d-output-%s", taskIndex+1, runNumber, stamp)
	if agent != "" {
		name += "-" + sanitizeFilePart(agent)
	}
	return name + ".txt"
}

func cleanTextOutputName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return ""
	}
	if filepath.Ext(name) == "" {
		name += ".txt"
	}
	return name
}
