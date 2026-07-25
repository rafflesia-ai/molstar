package cli

import "github.com/sacha-ichbiah/molstar/internal/job"

func serveOpenAPISchema() map[string]any {
	errorSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok": map[string]any{"type": "boolean"},
			"error": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code":      map[string]any{"type": "string"},
					"message":   map[string]any{"type": "string"},
					"diagnosis": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
			"timestamp": map[string]any{"type": "string", "format": "date-time"},
		},
	}
	jobSchema := job.JSONSchema()
	jobRef := map[string]any{"$ref": "#/components/schemas/Job"}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Headless Molstar Server API",
			"version":     buildVersion,
			"description": "HTTP and JSON-RPC API for validating, explaining, rendering, and monitoring headless Mol* jobs.",
		},
		"servers": []map[string]string{{"url": "http://127.0.0.1:8080"}},
		"paths": map[string]any{
			"/health": map[string]any{
				"get": endpoint("Health", "Check process health and queue state.", nil, response(200, "Health response", objectSchema())),
			},
			"/ready": map[string]any{
				"get": endpoint("Readiness", "Check prewarm readiness.", nil, response(200, "Ready", objectSchema()), response(503, "Not ready", objectSchema())),
			},
			"/metrics": map[string]any{
				"get": endpoint("Metrics", "Return JSON metrics for queue wait, render duration, worker restarts, and failures by error code.", nil, response(200, "Metrics", objectSchema())),
			},
			"/metrics/prometheus": map[string]any{
				"get": endpoint("Prometheus Metrics", "Return Prometheus text metrics for queue wait, render duration, worker fallbacks, and failures by error code.", nil, responseWithContent(200, "Prometheus metrics", "text/plain", map[string]any{"type": "string"})),
			},
			"/schema": map[string]any{
				"get": endpoint("Job Schema", "Return the headless job JSON schema.", nil, response(200, "Job schema", objectSchema())),
			},
			"/schema/openapi": map[string]any{
				"get": endpoint("OpenAPI Schema", "Return this OpenAPI document.", nil, response(200, "OpenAPI schema", objectSchema())),
			},
			"/capabilities": map[string]any{
				"get": endpoint("Capabilities", "Return renderer, runtime, worker, readiness, and server capability information.", nil, response(200, "Capabilities", objectSchema())),
			},
			"/validate": map[string]any{
				"post": endpoint("Validate", "Validate a job or recipe body.", jobRef, response(200, "Validation result", objectSchema()), response(400, "Invalid input", errorSchema)),
			},
			"/explain": map[string]any{
				"post": endpoint("Explain", "Compile a job into an execution explanation without rendering.", jobRef, response(200, "Explain result", objectSchema()), response(400, "Invalid input", errorSchema)),
			},
			"/render": map[string]any{
				"post": withCodeSamples(endpoint("Render", "Submit a render job. Use query async=true for asynchronous submission and dry_run=true for planning.", jobRef, response(200, "Render result", objectSchema()), response(202, "Async job", objectSchema()), response(429, "Server busy", errorSchema)), renderCodeSamples()),
			},
			"/rpc": map[string]any{
				"post": withCodeSamples(endpoint("JSON-RPC", "Call capabilities, metrics, validate, explain, or render over JSON-RPC 2.0.", objectSchema(), response(200, "JSON-RPC response", objectSchema())), rpcCodeSamples()),
			},
			"/jobs/{id}": map[string]any{
				"get":    endpoint("Job Status", "Fetch an async job status.", nil, response(200, "Job status", objectSchema()), response(404, "Job not found", errorSchema)),
				"delete": endpoint("Cancel Job", "Cancel a queued or running async job.", nil, response(202, "Cancel accepted", objectSchema()), response(404, "Job not found", errorSchema)),
			},
			"/jobs/{id}/events": map[string]any{
				"get": endpoint("Job Events", "Stream job events as JSON Lines.", nil, response(200, "Event stream", map[string]any{"type": "string", "contentMediaType": "application/jsonl"}), response(404, "Job not found", errorSchema)),
			},
			"/jobs/{id}/outputs": map[string]any{
				"get": endpoint("Job Outputs", "List output files recorded for a completed job.", nil, response(200, "Output list", objectSchema()), response(404, "Job not found", errorSchema)),
			},
			"/jobs/{id}/outputs/{index}": map[string]any{
				"get": endpoint("Download Job Output", "Download one output file by zero-based output index.", nil, response(200, "Output bytes", map[string]any{"type": "string", "contentEncoding": "binary"}), response(404, "Output not found", errorSchema)),
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"Job":       jobSchema,
				"Error":     errorSchema,
				"AnyObject": objectSchema(),
			},
			"examples": openAPIExamples(),
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"},
			},
		},
		"security": []map[string][]string{{"bearerAuth": []string{}}},
	}
}

func endpoint(summary string, description string, requestSchema map[string]any, responses ...map[string]any) map[string]any {
	result := map[string]any{
		"summary":     summary,
		"description": description,
		"responses":   mergeResponses(responses...),
	}
	if requestSchema != nil {
		result["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{"schema": requestSchema},
			},
		}
	}
	return result
}

func withCodeSamples(operation map[string]any, samples []map[string]string) map[string]any {
	operation["x-codeSamples"] = samples
	return operation
}

func response(status int, description string, schema map[string]any) map[string]any {
	return responseWithContent(status, description, "application/json", schema)
}

func responseWithContent(status int, description string, mediaType string, schema map[string]any) map[string]any {
	return map[string]any{
		"status": status,
		"value": map[string]any{
			"description": description,
			"content": map[string]any{
				mediaType: map[string]any{"schema": schema},
			},
		},
	}
}

func mergeResponses(responses ...map[string]any) map[string]any {
	result := map[string]any{}
	for _, response := range responses {
		status, ok := response["status"].(int)
		if !ok {
			continue
		}
		result[httpStatusText(status)] = response["value"]
	}
	return result
}

func objectSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}
}

func httpStatusText(status int) string {
	switch status {
	case 200:
		return "200"
	case 202:
		return "202"
	case 400:
		return "400"
	case 404:
		return "404"
	case 429:
		return "429"
	case 503:
		return "503"
	default:
		return "default"
	}
}

func openAPIExamples() map[string]any {
	renderJob := map[string]any{
		"version": 1,
		"inputs": map[string]any{
			"protein": map[string]any{"id": "1cbs", "provider": "pdbe"},
		},
		"scene": map[string]any{
			"canvas": map[string]any{"background": "white"},
			"structures": []map[string]any{{
				"source": "protein",
				"components": []map[string]any{
					{
						"ref":            "polymer",
						"select":         "polymer",
						"representation": map[string]any{"type": "cartoon", "color": "chain"},
					},
					{
						"ref":            "ligand",
						"select":         "ligand",
						"representation": map[string]any{"type": "ball-and-stick", "color": "#cc3399"},
					},
				},
			}},
			"camera": map[string]any{"focus": "ligand"},
		},
		"outputs": []map[string]any{{
			"type": "image",
			"path": "1cbs.png",
			"size": []int{1600, 1200},
		}},
	}
	return map[string]any{
		"RenderJob": map[string]any{
			"summary": "Render a styled PDB entry",
			"value":   renderJob,
		},
		"AsyncRenderResponse": map[string]any{
			"summary": "Accepted async render response",
			"value": map[string]any{
				"id":     "job_1",
				"status": "queued",
				"events": []map[string]string{{
					"phase":   "queued",
					"message": "job accepted",
				}},
			},
		},
		"JSONRPCMetricsRequest": map[string]any{
			"summary": "JSON-RPC metrics request",
			"value": map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "metrics",
			},
		},
		"CurlRender": map[string]any{
			"summary": "curl render request",
			"value":   renderCodeSamples()[0]["source"],
		},
		"PythonRender": map[string]any{
			"summary": "Python render request",
			"value":   renderCodeSamples()[1]["source"],
		},
		"NodeRender": map[string]any{
			"summary": "Node render request",
			"value":   renderCodeSamples()[2]["source"],
		},
	}
}

func renderCodeSamples() []map[string]string {
	return []map[string]string{
		{
			"lang":   "curl",
			"label":  "curl",
			"source": "curl -sS -X POST http://127.0.0.1:8080/render?async=true -H 'Content-Type: application/json' --data @job.json",
		},
		{
			"lang":  "Python",
			"label": "Python",
			"source": `import json
import urllib.request

with open("job.json", "rb") as f:
    request = urllib.request.Request(
        "http://127.0.0.1:8080/render?async=true",
        data=f.read(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
with urllib.request.urlopen(request) as response:
    print(json.dumps(json.load(response), indent=2))`,
		},
		{
			"lang":  "JavaScript",
			"label": "Node fetch",
			"source": `const fs = require("node:fs/promises");

async function main() {
  const body = await fs.readFile("job.json", "utf8");
  const response = await fetch("http://127.0.0.1:8080/render?async=true", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body,
  });
  console.log(JSON.stringify(await response.json(), null, 2));
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});`,
		},
	}
}

func rpcCodeSamples() []map[string]string {
	return []map[string]string{
		{
			"lang":   "curl",
			"label":  "curl metrics",
			"source": `curl -sS -X POST http://127.0.0.1:8080/rpc -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","id":1,"method":"metrics"}'`,
		},
	}
}
