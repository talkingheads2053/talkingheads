package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// sceneStep is a run of actor lines followed by the text to show before the
// next actor line.
type sceneStep struct {
	Lines   []question
	Display []string
}

// loadScene reads a scene file and parses it into steps.
func loadScene(path string, actorList []string) ([]sceneStep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	steps := parseScene(string(data), actorList)
	if len(steps) == 0 {
		return nil, fmt.Errorf("scene %s is empty", path)
	}
	return steps, nil
}

// parseScene splits a screenplay into steps. A paragraph that is only an
// actor name makes the next paragraph that actor's line.
func parseScene(text string, actorList []string) []sceneStep {
	paras := splitParagraphs(text)
	var steps []sceneStep
	for i := 0; i < len(paras); i++ {
		name := strings.ToLower(paras[i])
		if i+1 < len(paras) && slices.Contains(actorList, name) {
			if len(steps) == 0 || len(steps[len(steps)-1].Display) > 0 {
				steps = append(steps, sceneStep{})
			}
			step := &steps[len(steps)-1]
			content := strings.Join(strings.Fields(paras[i+1]), " ")
			step.Lines = append(step.Lines, question{To: name, Content: content, Kind: kindSay})
			i++
			continue
		}
		if len(steps) == 0 {
			steps = append(steps, sceneStep{})
		}
		step := &steps[len(steps)-1]
		step.Display = append(step.Display, paras[i])
	}
	return steps
}

func splitParagraphs(text string) []string {
	var paras, current []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			current = append(current, line)
			continue
		}
		if len(current) > 0 {
			paras = append(paras, strings.Join(current, "\n"))
			current = nil
		}
	}
	if len(current) > 0 {
		paras = append(paras, strings.Join(current, "\n"))
	}
	return paras
}

// parseSceneCommand returns the file named in "scene <file>" and reports
// whether text is a scene command.
func parseSceneCommand(text string, actorList []string) (string, bool) {
	first, rest, _ := strings.Cut(strings.TrimSpace(text), " ")
	if !strings.EqualFold(first, "scene") || slices.Contains(actorList, "scene") {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
