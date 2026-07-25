package cli

import "fmt"

func rendererGLStatus(renderer map[string]any) (available bool, detail string, present bool) {
	glValue, ok := renderer["gl"]
	if !ok {
		return false, "", false
	}
	gl, ok := glValue.(map[string]any)
	if !ok {
		return false, "", false
	}
	available, _ = gl["available"].(bool)
	if errText, ok := gl["error"].(string); ok {
		detail = errText
	}
	return available, detail, true
}

func rendererGLDetail(renderer map[string]any) string {
	available, detail, present := rendererGLStatus(renderer)
	if !present {
		return ""
	}
	if available {
		return "WebGL available"
	}
	if detail != "" {
		return fmt.Sprintf("WebGL unavailable: %s", detail)
	}
	return "WebGL unavailable"
}
