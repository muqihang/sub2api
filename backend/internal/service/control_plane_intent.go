package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	controlPlaneIntentAllowedFields = map[string]struct{}{
		"method":                  {},
		"path_template":           {},
		"normalized_query":        {},
		"query_ref":               {},
		"query_omitted_reason":    {},
		"classification":          {},
		"policy_version":          {},
		"strategy_version":        {},
		"response_schema_version": {},
		"routing_intent":          {},
		"body_length_bucket":      {},
		"schema_summary":          {},
		"body_omitted_reason":     {},
		"digest_omitted_reason":   {},
		"redaction_proof":         {},
	}
	controlPlaneIntentForbiddenFields = map[string]struct{}{
		"account_uuid":     {},
		"authorization":    {},
		"body":             {},
		"body_hash":        {},
		"cch":              {},
		"cookie":           {},
		"email":            {},
		"org_uuid":         {},
		"prompt":           {},
		"proxy_credential": {},
		"query_hash":       {},
		"raw_body":         {},
		"raw_cch":          {},
		"raw_prompt":       {},
		"raw_query":        {},
		"raw_telemetry":    {},
		"telemetry":        {},
		"user_uuid":        {},
		"x_api_key":        {},
	}
	controlPlaneAllowedBodyBuckets = map[string]struct{}{
		"empty":            {},
		"1_255_bytes":      {},
		"256_1023_bytes":   {},
		"1024_4095_bytes":  {},
		"4096_16383_bytes": {},
		"16384_plus_bytes": {},
	}
	controlPlaneAllowedQueryOmittedReasons = map[string]struct{}{"no_query": {}}
	controlPlaneAllowedBodyOmittedReasons  = map[string]struct{}{"not_applicable": {}, "high_risk_body_not_retained": {}, "empty_high_risk_body": {}}
	controlPlaneAllowedDigestOmissions     = map[string]struct{}{"not_applicable": {}, "raw_body_digest_forbidden_by_policy": {}}
	controlPlaneAllowedPlaceholders        = map[string]struct{}{"account": {}, "org": {}, "user": {}}
	controlPlaneIdentifierRe               = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	controlPlaneRefIdentifierRe            = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	controlPlaneHMACRefRe                  = regexp.MustCompile(`^hmac-sha256:[0-9a-f]{64}$`)
	controlPlanePlainSHARe                 = regexp.MustCompile(`^sha(?:1|224|256|384|512):[0-9a-f]{40,128}$`)
	controlPlanePlainMD5Re                 = regexp.MustCompile(`^md5:[0-9a-f]{32}$`)
	controlPlaneUUIDRe                     = regexp.MustCompile(`^(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[0-9a-fA-F]{32})$`)
	controlPlaneEmailRe                    = regexp.MustCompile(`[^@\s]+@[^@\s]+\.[^@\s]+`)
)

type ControlPlaneIntent struct {
	Method                string                     `json:"method"`
	PathTemplate          string                     `json:"path_template"`
	NormalizedQuery       map[string]string          `json:"normalized_query"`
	QueryRef              *ControlPlaneScopedHMACRef `json:"query_ref"`
	QueryOmittedReason    *string                    `json:"query_omitted_reason"`
	Classification        string                     `json:"classification"`
	PolicyVersion         int                        `json:"policy_version"`
	StrategyVersion       int                        `json:"strategy_version"`
	ResponseSchemaVersion int                        `json:"response_schema_version"`
	RoutingIntent         string                     `json:"routing_intent"`
	BodyLengthBucket      string                     `json:"body_length_bucket"`
	SchemaSummary         map[string]any             `json:"schema_summary"`
	BodyOmittedReason     string                     `json:"body_omitted_reason"`
	DigestOmittedReason   string                     `json:"digest_omitted_reason"`
	RedactionProof        ControlPlaneRedactionProof `json:"redaction_proof"`
}

type ControlPlaneScopedHMACRef struct {
	KeyID   string `json:"key_id"`
	Scope   string `json:"scope"`
	Version int    `json:"version"`
	Value   string `json:"value"`
}

type ControlPlaneRedactionProof struct {
	SensitiveScan           string `json:"sensitive_scan"`
	PathIdentifiersRedacted bool   `json:"path_identifiers_redacted"`
	RawQueryPersisted       bool   `json:"raw_query_persisted"`
	BodyPersisted           bool   `json:"body_persisted"`
	RawBodyDigestPersisted  bool   `json:"raw_body_digest_persisted"`
}

type ControlPlaneIntentAudit struct {
	Classification      string            `json:"classification"`
	PathTemplate        string            `json:"path_template"`
	NormalizedQuery     map[string]string `json:"normalized_query"`
	QueryOmittedReason  string            `json:"query_omitted_reason,omitempty"`
	BodyLengthBucket    string            `json:"body_length_bucket"`
	SchemaSummary       map[string]any    `json:"schema_summary"`
	BodyOmittedReason   string            `json:"body_omitted_reason"`
	DigestOmittedReason string            `json:"digest_omitted_reason"`
	PolicyDecision      string            `json:"policy_decision"`
}

type ControlPlaneIntentDecision struct {
	Decision    string                  `json:"decision"`
	Reason      string                  `json:"reason"`
	Status      int                     `json:"status"`
	ContentType string                  `json:"content_type,omitempty"`
	Body        any                     `json:"body,omitempty"`
	Audit       ControlPlaneIntentAudit `json:"audit"`
}

type ControlPlaneIntentService struct {
	matrix *ControlPlanePathPolicyMatrix
}

type ControlPlaneIntentServiceOption func(*ControlPlaneIntentService)

func NewControlPlaneIntentService(opts ...ControlPlaneIntentServiceOption) *ControlPlaneIntentService {
	svc := &ControlPlaneIntentService{matrix: NewDefaultControlPlanePathPolicyMatrix()}
	for _, opt := range opts {
		if opt != nil {
			opt(svc)
		}
	}
	return svc
}

func WithControlPlaneIntentPolicyMatrix(matrix *ControlPlanePathPolicyMatrix) ControlPlaneIntentServiceOption {
	return func(svc *ControlPlaneIntentService) {
		if matrix != nil {
			svc.matrix = matrix
		}
	}
}

func NewControlPlaneIntentServiceFromEnv() (*ControlPlaneIntentService, error) {
	matrix, err := NewControlPlanePathPolicyMatrixFromEnv()
	if err != nil {
		return nil, err
	}
	return NewControlPlaneIntentService(WithControlPlaneIntentPolicyMatrix(matrix)), nil
}

func ParseAndValidateControlPlaneIntent(body []byte) (*ControlPlaneIntent, error) {
	return parseAndValidateControlPlaneIntent(body)
}

func (s *ControlPlaneIntentService) ParseAndValidateIntent(body []byte) (*ControlPlaneIntent, error) {
	intent, err := parseControlPlaneIntentStrict(body)
	if err != nil {
		return nil, err
	}
	if err := s.validateIntent(intent); err != nil {
		return nil, err
	}
	return intent, nil
}

func (s *ControlPlaneIntentService) EvaluateIntent(body []byte) (*ControlPlaneIntentDecision, error) {
	intent, err := s.ParseAndValidateIntent(body)
	if err != nil {
		return nil, err
	}
	return s.EvaluateParsedIntent(intent), nil
}

func (s *ControlPlaneIntentService) EvaluateParsedIntent(intent *ControlPlaneIntent) *ControlPlaneIntentDecision {
	decision := s.decide(intent)
	decision.Audit = ControlPlaneIntentAudit{
		Classification:      intent.Classification,
		PathTemplate:        intent.PathTemplate,
		NormalizedQuery:     copyStringMap(intent.NormalizedQuery),
		BodyLengthBucket:    intent.BodyLengthBucket,
		SchemaSummary:       copyAnyMap(intent.SchemaSummary),
		BodyOmittedReason:   intent.BodyOmittedReason,
		DigestOmittedReason: intent.DigestOmittedReason,
		PolicyDecision:      decision.Decision,
	}
	if intent.QueryOmittedReason != nil {
		decision.Audit.QueryOmittedReason = *intent.QueryOmittedReason
	}
	return decision
}

func parseAndValidateControlPlaneIntent(body []byte) (*ControlPlaneIntent, error) {
	intent, err := parseControlPlaneIntentStrict(body)
	if err != nil {
		return nil, err
	}
	if err := validateControlPlaneIntent(intent); err != nil {
		return nil, err
	}
	return intent, nil
}

func parseControlPlaneIntentStrict(body []byte) (*ControlPlaneIntent, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("control-plane intent must be valid json object")
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("control-plane intent must be a non-empty object")
	}
	for field := range raw {
		if _, forbidden := controlPlaneIntentForbiddenFields[field]; forbidden {
			return nil, fmt.Errorf("control-plane intent contains forbidden field")
		}
		if _, allowed := controlPlaneIntentAllowedFields[field]; !allowed {
			return nil, fmt.Errorf("control-plane intent contains unknown field")
		}
	}
	if len(raw) != len(controlPlaneIntentAllowedFields) {
		return nil, fmt.Errorf("control-plane intent must match the strict allowlist schema")
	}
	var intent ControlPlaneIntent
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil {
		return nil, fmt.Errorf("control-plane intent schema decode failed")
	}
	return &intent, nil
}

func (s *ControlPlaneIntentService) validateIntent(intent *ControlPlaneIntent) error {
	if s == nil || s.matrix == nil || intent == nil || intent.Method != "GET" {
		return validateControlPlaneIntent(intent)
	}
	if _, exists := s.matrix.policies[controlPlanePolicyKey(intent.Method, intent.PathTemplate)]; !exists {
		return validateControlPlaneIntent(intent)
	}
	if err := validateControlPlaneIntentShapeExceptQuery(intent); err != nil {
		return err
	}
	rawQuery := url.Values{}
	for key, value := range intent.NormalizedQuery {
		rawQuery.Set(key, value)
	}
	decision := s.matrix.Evaluate(intent.Method, intent.PathTemplate, rawQuery.Encode())
	if decision.Policy == nil {
		return fmt.Errorf("control-plane intent path is not allowlisted")
	}
	if len(intent.NormalizedQuery) > 0 {
		if !controlPlaneEqualStringMaps(decision.NormalizedQuery, intent.NormalizedQuery) {
			return fmt.Errorf("control-plane intent normalized_query does not match configured policy")
		}
		if err := validateQueryRef(intent.QueryRef); err != nil {
			return err
		}
		if intent.QueryOmittedReason != nil {
			return fmt.Errorf("control-plane intent query_omitted_reason must be omitted when normalized_query is present")
		}
	} else if err := validateControlPlaneQuery(intent.PathTemplate, intent.NormalizedQuery, intent.QueryRef, intent.QueryOmittedReason); err != nil {
		return err
	}
	return nil
}

func validateControlPlaneIntentShapeExceptQuery(intent *ControlPlaneIntent) error {
	if intent == nil {
		return fmt.Errorf("control-plane intent is required")
	}
	savedQuery := intent.NormalizedQuery
	savedQueryRef := intent.QueryRef
	savedOmitted := intent.QueryOmittedReason
	intent.NormalizedQuery = map[string]string{}
	noQuery := "no_query"
	intent.QueryRef = nil
	intent.QueryOmittedReason = &noQuery
	err := validateControlPlaneIntent(intent)
	intent.NormalizedQuery = savedQuery
	intent.QueryRef = savedQueryRef
	intent.QueryOmittedReason = savedOmitted
	return err
}

func controlPlaneEqualStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func validateControlPlaneIntent(intent *ControlPlaneIntent) error {
	if intent == nil {
		return fmt.Errorf("control-plane intent is required")
	}
	if intent.Method != "GET" && intent.Method != "POST" {
		return fmt.Errorf("control-plane intent method must be GET or POST")
	}
	if err := validateSafeIdentifier(intent.Classification, "classification"); err != nil {
		return err
	}
	if err := validateSafeIdentifier(intent.RoutingIntent, "routing_intent"); err != nil {
		return err
	}
	if err := validatePathTemplate(intent.PathTemplate); err != nil {
		return err
	}
	if intent.PolicyVersion <= 0 || intent.StrategyVersion <= 0 || intent.ResponseSchemaVersion <= 0 {
		return fmt.Errorf("control-plane intent versions must be positive")
	}
	if _, ok := controlPlaneAllowedBodyBuckets[intent.BodyLengthBucket]; !ok {
		return fmt.Errorf("control-plane intent body_length_bucket is invalid")
	}
	if _, ok := controlPlaneAllowedBodyOmittedReasons[intent.BodyOmittedReason]; !ok {
		return fmt.Errorf("control-plane intent body_omitted_reason is invalid")
	}
	if _, ok := controlPlaneAllowedDigestOmissions[intent.DigestOmittedReason]; !ok {
		return fmt.Errorf("control-plane intent digest_omitted_reason is invalid")
	}
	if intent.BodyLengthBucket == "empty" && intent.BodyOmittedReason == "high_risk_body_not_retained" {
		return fmt.Errorf("control-plane intent body metadata is inconsistent")
	}
	if err := validateControlPlaneQuery(intent.PathTemplate, intent.NormalizedQuery, intent.QueryRef, intent.QueryOmittedReason); err != nil {
		return err
	}
	if err := validateSafeJSONValue(intent.SchemaSummary, "schema_summary"); err != nil {
		return err
	}
	if err := validateRedactionProof(intent.RedactionProof); err != nil {
		return err
	}
	return nil
}

func (s *ControlPlaneIntentService) decide(intent *ControlPlaneIntent) *ControlPlaneIntentDecision {
	if s == nil {
		s = NewControlPlaneIntentService()
	}
	if s.matrix != nil && intent.Method == "GET" {
		query := url.Values{}
		for key, value := range intent.NormalizedQuery {
			query.Set(key, value)
		}
		matrixDecision := s.matrix.Evaluate(intent.Method, intent.PathTemplate, query.Encode())
		if matrixDecision.Policy != nil || matrixDecision.Reason != "control_plane:path_not_allowlisted" {
			decision := &ControlPlaneIntentDecision{
				Decision: matrixDecision.Decision,
				Reason:   matrixDecision.Reason,
				Status:   matrixDecision.Status,
			}
			if matrixDecision.Policy != nil && matrixDecision.Policy.Action == ControlPlaneActionStub {
				decision.ContentType = "application/json"
				decision.Body = safeControlPlaneStubBody(matrixDecision.Policy)
			}
			return decision
		}
	}
	if intent.Method == "POST" && (intent.PathTemplate == "/api/event_logging/v2/batch" || strings.HasPrefix(intent.PathTemplate, "/api/eval/")) {
		return &ControlPlaneIntentDecision{
			Decision: "suppress_204",
			Reason:   "control_plane:telemetry_or_eval:path",
			Status:   204,
		}
	}
	if intent.Method == "GET" {
		switch intent.PathTemplate {
		case "/v1/mcp_servers":
			return &ControlPlaneIntentDecision{
				Decision:    "stub_json",
				Reason:      "control_plane:mcp_or_registry:path",
				Status:      200,
				ContentType: "application/json",
				Body:        map[string]any{"data": []any{}, "servers": []any{}},
			}
		case "/api/claude_cli/bootstrap":
			return &ControlPlaneIntentDecision{
				Decision:    "stub_json",
				Reason:      "control_plane:bootstrap_settings:path",
				Status:      200,
				ContentType: "application/json",
				Body:        map[string]any{},
			}
		case "/api/oauth/account/settings":
			return &ControlPlaneIntentDecision{
				Decision: "quarantine_block",
				Reason:   "control_plane:account_settings_sensitive:no_stale_no_fallback",
				Status:   403,
			}
		}
	}
	return &ControlPlaneIntentDecision{
		Decision: "quarantine_block",
		Reason:   "control_plane:unknown_path:quarantine",
		Status:   403,
	}
}

func safeControlPlaneStubBody(policy *ControlPlanePathPolicy) map[string]any {
	body := map[string]any{}
	if policy == nil {
		return body
	}
	if _, ok := policy.AllowedResponseKeys["ok"]; ok {
		body["ok"] = true
	}
	if _, ok := policy.AllowedResponseKeys["data"]; ok {
		body["data"] = []any{}
	}
	if _, ok := policy.AllowedResponseKeys["servers"]; ok {
		body["servers"] = []any{}
	}
	if _, ok := policy.AllowedResponseKeys["features"]; ok {
		body["features"] = []any{}
	}
	if _, ok := policy.AllowedResponseKeys["profile"]; ok {
		body["profile"] = map[string]any{}
	}
	return body
}

func validateControlPlaneQuery(pathTemplate string, normalizedQuery map[string]string, queryRef *ControlPlaneScopedHMACRef, queryOmittedReason *string) error {
	allowed := allowedControlPlaneQueryRules(pathTemplate)
	if len(normalizedQuery) > 0 {
		if allowed == nil {
			return fmt.Errorf("control-plane intent normalized_query is not allowed for this path")
		}
		for key, value := range normalizedQuery {
			rule, ok := allowed[key]
			if !ok {
				return fmt.Errorf("control-plane intent normalized_query contains a non-allowlisted key")
			}
			if err := validateNormalizedQueryValue(key, value, rule); err != nil {
				return err
			}
		}
		if err := validateQueryRef(queryRef); err != nil {
			return err
		}
		if queryOmittedReason != nil {
			return fmt.Errorf("control-plane intent query_omitted_reason must be omitted when normalized_query is present")
		}
		return nil
	}
	if queryRef != nil {
		return fmt.Errorf("control-plane intent query_ref must be omitted when normalized_query is empty")
	}
	if queryOmittedReason == nil {
		return fmt.Errorf("control-plane intent query_omitted_reason is required when normalized_query is empty")
	}
	if _, ok := controlPlaneAllowedQueryOmittedReasons[*queryOmittedReason]; !ok {
		return fmt.Errorf("control-plane intent query_omitted_reason is invalid")
	}
	return nil
}

func validateQueryRef(ref *ControlPlaneScopedHMACRef) error {
	if ref == nil {
		return fmt.Errorf("control-plane intent query_ref is required")
	}
	if !controlPlaneRefIdentifierRe.MatchString(ref.KeyID) || looksSensitiveText(ref.KeyID) {
		return fmt.Errorf("control-plane intent query_ref.key_id is invalid")
	}
	if ref.Scope != "control_plane_query" {
		return fmt.Errorf("control-plane intent query_ref.scope is invalid")
	}
	if ref.Version <= 0 {
		return fmt.Errorf("control-plane intent query_ref.version is invalid")
	}
	if !controlPlaneHMACRefRe.MatchString(ref.Value) {
		return fmt.Errorf("control-plane intent query_ref.value must use scoped hmac-sha256")
	}
	return nil
}

func validateNormalizedQueryValue(key, value, rule string) error {
	if looksSensitiveText(value) || looksPlainDigest(value) {
		return fmt.Errorf("control-plane intent normalized_query contains sensitive content")
	}
	if strings.HasPrefix(rule, "enum:") {
		allowed := strings.Split(strings.TrimPrefix(rule, "enum:"), "|")
		for _, candidate := range allowed {
			if value == candidate {
				return nil
			}
		}
		return fmt.Errorf("control-plane intent normalized_query contains a non-allowlisted value")
	}
	if strings.HasPrefix(rule, "int:") {
		parts := strings.Split(rule, ":")
		if len(parts) != 3 {
			return fmt.Errorf("control-plane query rule is invalid")
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("control-plane intent normalized_query contains a non-allowlisted value")
		}
		lower, _ := strconv.Atoi(parts[1])
		upper, _ := strconv.Atoi(parts[2])
		if parsed < lower || parsed > upper {
			return fmt.Errorf("control-plane intent normalized_query contains a non-allowlisted value")
		}
		return nil
	}
	return fmt.Errorf("control-plane query rule is invalid")
}

func allowedControlPlaneQueryRules(pathTemplate string) map[string]string {
	switch pathTemplate {
	case "/api/claude_cli/bootstrap":
		return map[string]string{"entrypoint": "enum:sdk-cli"}
	case "/v1/mcp_servers":
		return map[string]string{"limit": "int:1:1000"}
	default:
		return nil
	}
}

func validatePathTemplate(pathTemplate string) error {
	if !strings.HasPrefix(pathTemplate, "/") {
		return fmt.Errorf("control-plane intent path_template must be absolute")
	}
	for _, segment := range strings.Split(pathTemplate, "/")[1:] {
		if segment == "" {
			continue
		}
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			placeholder := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
			if _, ok := controlPlaneAllowedPlaceholders[placeholder]; !ok {
				return fmt.Errorf("control-plane intent path_template placeholder is invalid")
			}
			continue
		}
		if looksSensitiveIdentifier(segment) || looksUnsafeDynamicIdentifier(segment) {
			return fmt.Errorf("control-plane intent path_template is unsafe")
		}
	}
	return nil
}

func validateSafeIdentifier(value, field string) error {
	if !controlPlaneIdentifierRe.MatchString(value) || looksSensitiveText(value) || looksPlainDigest(value) {
		return fmt.Errorf("control-plane intent %s is invalid", field)
	}
	return nil
}

func validateRedactionProof(proof ControlPlaneRedactionProof) error {
	if proof.SensitiveScan != "clean" {
		return fmt.Errorf("control-plane intent redaction_proof.sensitive_scan must be clean")
	}
	if proof.RawQueryPersisted || proof.BodyPersisted || proof.RawBodyDigestPersisted {
		return fmt.Errorf("control-plane intent redaction_proof must assert non-persistence")
	}
	return nil
}

func validateSafeJSONValue(value any, field string) error {
	switch typed := value.(type) {
	case nil, bool, float64, int, int32, int64, uint, uint32, uint64:
		return nil
	case string:
		if looksSensitiveText(typed) || looksPlainDigest(typed) || looksUnsafeDynamicIdentifier(typed) {
			return fmt.Errorf("control-plane intent %s contains sensitive content", field)
		}
		return nil
	case []any:
		for _, item := range typed {
			if err := validateSafeJSONValue(item, field); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for key, item := range typed {
			if looksSensitiveText(key) || looksPlainDigest(key) || looksUnsafeDynamicIdentifier(key) {
				return fmt.Errorf("control-plane intent %s contains sensitive content", field)
			}
			if err := validateSafeJSONValue(item, field); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("control-plane intent %s contains unsupported data", field)
	}
}

func looksUnsafeDynamicIdentifier(value string) bool {
	stripped := strings.TrimSpace(value)
	lowered := strings.ToLower(stripped)
	if controlPlaneUUIDRe.MatchString(stripped) || controlPlaneEmailRe.MatchString(stripped) {
		return true
	}
	if strings.HasPrefix(lowered, "local-org-") || strings.HasPrefix(lowered, "local-account-") || strings.HasPrefix(lowered, "local-user-") {
		return true
	}
	if matched, _ := regexp.MatchString(`^(?:account|org|organization|user|session|project)(?:[_-].+)$`, lowered); matched {
		return true
	}
	return strings.Contains(lowered, "org-secret") || strings.Contains(lowered, "account-secret") || strings.Contains(lowered, "user-secret") || strings.Contains(lowered, "session-id")
}

func looksSensitiveIdentifier(value string) bool {
	return controlPlaneEmailRe.MatchString(value) || containsSensitiveMarker(value)
}

func looksSensitiveText(value string) bool {
	return controlPlaneEmailRe.MatchString(value) || containsSensitiveMarker(value)
}

func looksPlainDigest(value string) bool {
	return controlPlanePlainSHARe.MatchString(strings.ToLower(value)) || controlPlanePlainMD5Re.MatchString(strings.ToLower(value))
}

func containsSensitiveMarker(value string) bool {
	lowered := strings.ToLower(value)
	if strings.HasPrefix(lowered, "sk-") {
		return true
	}
	replacer := strings.NewReplacer("-", " ", "_", " ", ".", " ", "/", " ")
	parts := strings.Fields(replacer.Replace(lowered))
	joined := strings.Join(parts, "")
	sensitiveParts := map[string]struct{}{
		"prompt": {}, "token": {}, "secret": {}, "cookie": {}, "credential": {}, "authorization": {},
	}
	for _, part := range parts {
		if _, ok := sensitiveParts[part]; ok {
			return true
		}
	}
	return strings.Contains(joined, "rawprompt") || strings.Contains(joined, "accesstoken") || strings.Contains(joined, "xapikey")
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func copyAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return map[string]any{}
	}
	return out
}
