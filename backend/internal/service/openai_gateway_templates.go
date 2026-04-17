package service

import "strings"

type OpenAIGatewayClientTemplates struct {
	APIBaseURL      string `json:"api_base_url"`
	ProbeBaseURL    string `json:"probe_base_url"`
	CurlExample     string `json:"curl_example"`
	PythonSDK       string `json:"python_sdk"`
	NodeSDK         string `json:"node_sdk"`
	CodexConfigTOML string `json:"codex_config_toml"`
	CodexAuthJSON   string `json:"codex_auth_json"`
	CodexWrapperSH  string `json:"codex_wrapper_sh"`
}

func BuildOpenAIGatewayClientTemplates(apiBaseURL, apiKey, gatewayToken string) *OpenAIGatewayClientTemplates {
	baseURL := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if baseURL == "" {
		baseURL = "https://example.com"
	}
	probeBaseURL := baseURL + "/openai"
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		apiKey = "<SUB2API_API_KEY>"
	}
	gatewayToken = strings.TrimSpace(gatewayToken)
	gatewayHeaderCurl := ""
	gatewayHeaderPython := ""
	gatewayHeaderNode := ""
	gatewayHeaderComment := "# optional gateway token not set"
	if gatewayToken != "" {
		gatewayHeaderCurl = "\n  -H \"X-OpenAI-Gateway-Token: " + gatewayToken + "\""
		gatewayHeaderPython = ",\n    default_headers={\"X-OpenAI-Gateway-Token\": \"" + gatewayToken + "\"}"
		gatewayHeaderNode = ",\n  defaultHeaders: { \"X-OpenAI-Gateway-Token\": \"" + gatewayToken + "\" }"
		gatewayHeaderComment = "export OPENAI_GATEWAY_TOKEN=\"" + gatewayToken + "\""
	}

	return &OpenAIGatewayClientTemplates{
		APIBaseURL:   baseURL,
		ProbeBaseURL: probeBaseURL,
		CurlExample: "curl " + baseURL + "/v1/responses \\\n" +
			"  -H \"Authorization: Bearer " + apiKey + "\" \\\n" +
			"  -H \"Content-Type: application/json\"" + gatewayHeaderCurl + " \\\n" +
			"  -d '{\"model\":\"gpt-5.4\",\"input\":\"hello\"}'",
		PythonSDK: "from openai import OpenAI\n\n" +
			"client = OpenAI(\n" +
			"    base_url=\"" + baseURL + "\",\n" +
			"    api_key=\"" + apiKey + "\"" + gatewayHeaderPython + "\n" +
			")\n\n" +
			"resp = client.responses.create(model=\"gpt-5.4\", input=\"hello\")\n" +
			"print(resp.output_text)",
		NodeSDK: "import OpenAI from \"openai\";\n\n" +
			"const client = new OpenAI({\n" +
			"  baseURL: \"" + baseURL + "\",\n" +
			"  apiKey: \"" + apiKey + "\"" + gatewayHeaderNode + "\n" +
			"});\n\n" +
			"const resp = await client.responses.create({ model: \"gpt-5.4\", input: \"hello\" });\n" +
			"console.log(resp.output_text);",
		CodexConfigTOML: "model_provider = \"OpenAI\"\n" +
			"model = \"gpt-5.4\"\n" +
			"review_model = \"gpt-5.4\"\n" +
			"model_reasoning_effort = \"xhigh\"\n" +
			"disable_response_storage = true\n" +
			"network_access = \"enabled\"\n\n" +
			"[model_providers.OpenAI]\n" +
			"name = \"OpenAI\"\n" +
			"base_url = \"" + baseURL + "\"\n" +
			"wire_api = \"responses\"\n" +
			"supports_websockets = true\n" +
			"requires_openai_auth = true\n",
		CodexAuthJSON: "{\n  \"OPENAI_API_KEY\": \"" + apiKey + "\"\n}",
		CodexWrapperSH: "#!/usr/bin/env bash\n" +
			"set -euo pipefail\n" +
			"export OPENAI_BASE_URL=\"" + baseURL + "\"\n" +
			"export OPENAI_API_KEY=\"" + apiKey + "\"\n" +
			gatewayHeaderComment + "\n" +
			"exec codex \"$@\"\n",
	}
}
