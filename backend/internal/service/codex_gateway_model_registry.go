package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type CodexGatewayModelRegistry struct {
	fallback             config.GatewayCodexConfig
	orderedSlugs         []string
	stateSource          CodexGatewayRegistryStateSource
	pricingChecker       CodexGatewayPricingReadyChecker
	protocolChecker      CodexGatewayProtocolReadyChecker
	variantChecker       CodexGatewayVariantReadyChecker
	modelPricingResolver CodexGatewayModelPricingResolver
}

func NewDefaultCodexGatewayModelRegistry() *CodexGatewayModelRegistry {
	return NewCodexGatewayModelRegistry(config.GatewayCodexConfig{
		EnabledModels: defaultCodexGatewayEnabledModelSlugs(),
		ProviderGroups: config.GatewayCodexProviderGroupsConfig{
			OpenAI: 1,
		},
	})
}

type CodexGatewayRegistryStateSource interface {
	LoadCodexGatewayRegistryState(ctx context.Context) (*CodexGatewayRegistryState, error)
}

type CodexGatewayModelPricingResolver interface {
	ResolveCodexGatewayModelPricing(ctx context.Context, model CodexGatewayModel, groupID *int64) *CodexGatewayModelPricing
}

type codexGatewayDatabaseModelPricingResolver struct {
	resolver *ModelPricingResolver
}

func NewCodexGatewayDatabaseModelPricingResolver(resolver *ModelPricingResolver) CodexGatewayModelPricingResolver {
	return codexGatewayDatabaseModelPricingResolver{resolver: resolver}
}

func (r codexGatewayDatabaseModelPricingResolver) ResolveCodexGatewayModelPricing(ctx context.Context, model CodexGatewayModel, groupID *int64) *CodexGatewayModelPricing {
	if r.resolver == nil {
		return nil
	}
	for _, modelName := range codexEntryPricingModelCandidates(model) {
		resolved := r.resolver.Resolve(ctx, PricingInput{Model: modelName, GroupID: groupID})
		if resolved == nil || resolved.Source != PricingSourceChannel {
			continue
		}
		pricing := codexGatewayResolvedPricingToCatalog(resolved)
		if pricing != nil {
			return pricing
		}
	}
	return nil
}

type CodexGatewayModelRegistryOption func(*CodexGatewayModelRegistry)

type CodexGatewayVariantReadyChecker interface {
	IsReady(ctx context.Context, model CodexGatewayModel, providerRuntime CodexGatewayProviderRuntime) bool
}

type CodexGatewayRegistryState struct {
	ProviderGroups map[CodexGatewayProvider]CodexGatewayProviderRuntime `json:"provider_groups"`
	Models         map[string]CodexGatewayModelMutation                 `json:"models"`
}

type codexGatewayRegistryComputedState struct {
	models []CodexGatewayModel
	index  map[string]CodexGatewayModel
}

type CodexGatewayCodexCLICatalog struct {
	Models []CodexGatewayCodexCLIModel `json:"models"`
}

type CodexGatewayCodexCLIModel struct {
	Slug                          string                            `json:"slug"`
	DisplayName                   string                            `json:"display_name"`
	Origin                        string                            `json:"origin"`
	ProviderID                    string                            `json:"provider_id"`
	Capabilities                  CodexGatewayModelCapabilities     `json:"capabilities"`
	Pricing                       *CodexGatewayModelPricing         `json:"pricing"`
	Description                   string                            `json:"description"`
	DefaultReasoningLevel         string                            `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels      []CodexGatewayReasoningLevelInfo  `json:"supported_reasoning_levels,omitempty"`
	ShellType                     string                            `json:"shell_type"`
	Visibility                    string                            `json:"visibility"`
	SupportedInAPI                bool                              `json:"supported_in_api"`
	Priority                      int                               `json:"priority"`
	BaseInstructions              string                            `json:"base_instructions"`
	ModelMessages                 CodexGatewayCodexCLIModelMessages `json:"model_messages"`
	ContextWindow                 int                               `json:"context_window,omitempty"`
	AutoCompactTokenLimit         int                               `json:"auto_compact_token_limit,omitempty"`
	MaxContextWindow              int                               `json:"max_context_window,omitempty"`
	EffectiveContextWindowPercent int                               `json:"effective_context_window_percent,omitempty"`
	MaxOutputTokens               int                               `json:"max_output_tokens,omitempty"`
	SupportVerbosity              bool                              `json:"support_verbosity"`
	DefaultVerbosity              string                            `json:"default_verbosity,omitempty"`
	ApplyPatchToolType            string                            `json:"apply_patch_tool_type,omitempty"`
	WebSearchToolType             string                            `json:"web_search_tool_type,omitempty"`
	TruncationPolicy              CodexGatewayCodexCLITruncation    `json:"truncation_policy"`
	SupportsParallelToolCalls     bool                              `json:"supports_parallel_tool_calls"`
	SupportsImageDetailOriginal   bool                              `json:"supports_image_detail_original"`
	SupportsReasoningSummaries    bool                              `json:"supports_reasoning_summaries"`
	DefaultReasoningSummary       string                            `json:"default_reasoning_summary,omitempty"`
	ExperimentalSupportedTools    []string                          `json:"experimental_supported_tools"`
	InputModalities               []string                          `json:"input_modalities"`
	SupportsSearchTool            bool                              `json:"supports_search_tool"`
}

type CodexGatewayReasoningLevelInfo struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type CodexGatewayCodexCLIModelMessages struct {
	InstructionsTemplate  string            `json:"instructions_template"`
	InstructionsVariables map[string]string `json:"instructions_variables"`
}

type CodexGatewayCodexCLITruncation struct {
	Mode  string `json:"mode"`
	Limit int    `json:"limit"`
}

const codexGatewayDefaultBaseInstructions = `You are Codex, based on GPT-5. You are running as a coding agent in the Codex CLI on a user's computer.

## General

- When searching for text or files, prefer using ` + "`rg`" + ` or ` + "`rg --files`" + ` respectively because ` + "`rg`" + ` is much faster than alternatives like ` + "`grep`" + `. (If the ` + "`rg`" + ` command is not found, then use alternatives.)
- Act as an agent: inspect the workspace and use available tools to complete the user's task rather than only describing changes.
- For multi-line file creation or rewrites, prefer a shell command such as ` + "`python3 - <<'PY' ... PY`" + ` when it is safer than many small edits; use ` + "`edit`" + ` or ` + "`apply_patch`" + ` for targeted changes.
- For quick environment checks, use shell commands like ` + "`pwd`" + `, ` + "`git status --short`" + `, ` + "`ls -la`" + `, and ` + "`rg --files`" + ` when relevant.

## Editing constraints

- Default to ASCII when editing or creating files. Only introduce non-ASCII or other Unicode characters when there is a clear justification and the file already uses them.
- Add succinct code comments that explain what is going on if code is not self-explanatory. You should not add comments like "Assigns the value to the variable", but a brief comment might be useful ahead of a complex code block that the user would otherwise have to spend time parsing out. Usage of these comments should be rare.
- Try to use ` + "`edit`" + ` for single file edits, but it is fine to explore other options if that does not fit well. Do not use ` + "`edit`" + ` for changes that are auto-generated (i.e. generating package.json or running a lint or format command like gofmt) or when scripting is more efficient.
- You may be in a dirty git worktree. NEVER revert existing changes you did not make unless explicitly requested.
- NEVER use destructive commands like ` + "`git reset --hard`" + ` or ` + "`git checkout --`" + ` unless specifically requested or approved by the user.

## Presenting your work

- Be concise and factual.
- For substantial work, summarize what changed and why.
- Offer next steps only when they are useful.
`

const codexGatewayProviderRoutingBridgeInstructions = `## Codex routing guidance

- When Codex developer instructions include skills, plugins, MCP servers, or tool routing guidance, treat those sections as active routing guidance.
- Before substantive work, quickly decide whether the user's request clearly matches any listed trigger. If it clearly matches, read only the relevant SKILL.md or use the relevant plugin, MCP server, or tool first, then continue.
- Do not load unrelated skills, do not repeatedly reload the same skill in the same turn, and do not use tools only for show.
`

func codexGatewayProviderNeedsRoutingBridge(model CodexGatewayModel) bool {
	switch normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider)) {
	case CodexGatewayProviderDeepSeek, CodexGatewayProviderAnthropic:
		return true
	default:
		return false
	}
}

func codexGatewayBaseInstructionsForModel(model CodexGatewayModel) string {
	if !codexGatewayProviderNeedsRoutingBridge(model) {
		return codexGatewayDefaultBaseInstructions
	}
	return strings.TrimRight(codexGatewayDefaultBaseInstructions, "\n") + "\n\n" + codexGatewayProviderRoutingBridgeInstructions
}

func WithCodexGatewayRegistryStateSource(source CodexGatewayRegistryStateSource) CodexGatewayModelRegistryOption {
	return func(registry *CodexGatewayModelRegistry) {
		registry.stateSource = source
	}
}

func WithCodexGatewayPricingReadyChecker(checker CodexGatewayPricingReadyChecker) CodexGatewayModelRegistryOption {
	return func(registry *CodexGatewayModelRegistry) {
		registry.pricingChecker = checker
	}
}

func WithCodexGatewayProtocolReadyChecker(checker CodexGatewayProtocolReadyChecker) CodexGatewayModelRegistryOption {
	return func(registry *CodexGatewayModelRegistry) {
		registry.protocolChecker = checker
	}
}

func WithCodexGatewayModelPricingResolver(resolver CodexGatewayModelPricingResolver) CodexGatewayModelRegistryOption {
	return func(registry *CodexGatewayModelRegistry) {
		registry.modelPricingResolver = resolver
	}
}

func WithCodexGatewayVariantReadyChecker(checker CodexGatewayVariantReadyChecker) CodexGatewayModelRegistryOption {
	return func(registry *CodexGatewayModelRegistry) {
		registry.variantChecker = checker
	}
}

func NewCodexGatewayModelRegistry(cfg config.GatewayCodexConfig, options ...CodexGatewayModelRegistryOption) *CodexGatewayModelRegistry {
	if len(cfg.EnabledModels) == 0 {
		cfg.EnabledModels = defaultCodexGatewayEnabledModelSlugs()
	}

	base := defaultCodexGatewayModels()
	orderedSlugs := make([]string, 0, len(base))
	for _, model := range base {
		orderedSlugs = append(orderedSlugs, model.Slug)
	}

	registry := &CodexGatewayModelRegistry{
		fallback:        cfg,
		orderedSlugs:    orderedSlugs,
		pricingChecker:  defaultCodexGatewayPricingReadyChecker,
		protocolChecker: defaultCodexGatewayProtocolReadyChecker,
		variantChecker:  codexGatewayVariantReadyAllowAll{},
	}
	for _, option := range options {
		if option != nil {
			option(registry)
		}
	}
	return registry
}

func (r *CodexGatewayModelRegistry) enabledSlugs() map[string]struct{} {
	base := defaultCodexGatewayModels()
	enabled := make(map[string]struct{}, len(base))
	cfg := r.fallback
	if len(cfg.EnabledModels) == 0 {
		for _, model := range base {
			enabled[model.Slug] = struct{}{}
		}
	} else {
		for _, modelID := range cfg.EnabledModels {
			modelID = strings.TrimSpace(modelID)
			if modelID == "" {
				continue
			}
			enabled[modelID] = struct{}{}
		}
	}
	return enabled
}

func (r *CodexGatewayModelRegistry) computeState() codexGatewayRegistryComputedState {
	state := codexGatewayRegistryComputedState{
		models: []CodexGatewayModel{},
		index:  make(map[string]CodexGatewayModel),
	}
	if r == nil {
		return state
	}

	registryState := &CodexGatewayRegistryState{
		ProviderGroups: buildFallbackCodexGatewayProviderGroups(r.fallback),
		Models:         buildFallbackCodexGatewayModelMutations(r.fallback),
	}
	if r.stateSource != nil {
		if loaded, err := r.stateSource.LoadCodexGatewayRegistryState(context.Background()); err == nil && loaded != nil {
			registryState = &CodexGatewayRegistryState{
				ProviderGroups: mergeCodexGatewayProviderGroups(buildFallbackCodexGatewayProviderGroups(r.fallback), loaded.ProviderGroups),
				Models:         mergeCodexGatewayModelMutations(buildFallbackCodexGatewayModelMutations(r.fallback), loaded.Models),
			}
		}
	}

	enabled := r.enabledSlugs()
	for _, model := range defaultCodexGatewayModels() {
		if _, ok := enabled[model.Slug]; !ok {
			continue
		}
		provider := normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider))
		mutation := registryState.Models[model.Slug]
		model = codexGatewayApplyVisibilityGates(
			model,
			mutation,
			registryState.ProviderGroups[provider],
			r.pricingChecker,
			r.protocolChecker,
			r.variantChecker,
		)
		state.models = append(state.models, model)
		state.index[model.Slug] = model
	}

	return state
}

func (r *CodexGatewayModelRegistry) AllModels() []CodexGatewayModel {
	state := r.computeState()
	models := make([]CodexGatewayModel, len(state.models))
	copy(models, state.models)
	return models
}

func (r *CodexGatewayModelRegistry) Models() []CodexGatewayModel {
	state := r.computeState()
	models := make([]CodexGatewayModel, 0, len(state.models))
	for _, model := range state.models {
		if !model.SupportedInAPI || model.Visibility != "visible" {
			continue
		}
		models = append(models, model)
	}
	return models
}

func (r *CodexGatewayModelRegistry) Resolve(slug string) (CodexGatewayModel, bool) {
	model, ok := r.computeState().index[strings.TrimSpace(slug)]
	return model, ok
}

func (r *CodexGatewayModelRegistry) ModelsResponse(groupID ...*int64) CodexGatewayModelsResponse {
	return CodexGatewayModelsResponse{Models: r.decorateModels(context.Background(), r.Models(), firstCodexGatewayGroupID(groupID))}
}

func (r *CodexGatewayModelRegistry) ExportCatalogJSON(groupID ...*int64) ([]byte, error) {
	return json.MarshalIndent(r.ModelsResponse(firstCodexGatewayGroupID(groupID)), "", "  ")
}

func (r *CodexGatewayModelRegistry) ExportCodexCLICatalogJSON(groupID ...*int64) ([]byte, error) {
	models := r.decorateModels(context.Background(), r.Models(), firstCodexGatewayGroupID(groupID))
	out := CodexGatewayCodexCLICatalog{
		Models: make([]CodexGatewayCodexCLIModel, 0, len(models)),
	}
	for _, model := range models {
		out.Models = append(out.Models, codexGatewayModelToCodexCLIModel(model))
	}
	return json.MarshalIndent(out, "", "  ")
}

func firstCodexGatewayGroupID(values []*int64) *int64 {
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

func (r *CodexGatewayModelRegistry) decorateModels(ctx context.Context, models []CodexGatewayModel, groupID *int64) []CodexGatewayModel {
	out := make([]CodexGatewayModel, 0, len(models))
	for _, model := range models {
		out = append(out, r.decorateModel(ctx, model, groupID))
	}
	return out
}

func (r *CodexGatewayModelRegistry) decorateModel(ctx context.Context, model CodexGatewayModel, groupID *int64) CodexGatewayModel {
	model.Origin = "zhumeng"
	model.ProviderID = "zhumeng"
	if r != nil && r.modelPricingResolver != nil {
		model.Pricing = r.modelPricingResolver.ResolveCodexGatewayModelPricing(ctx, model, groupID)
	}
	model.Capabilities = codexGatewayModelCapabilities(model)
	return model
}

func codexGatewayModelCapabilities(model CodexGatewayModel) CodexGatewayModelCapabilities {
	return CodexGatewayModelCapabilities{
		Responses:           true,
		Streaming:           true,
		ToolCalls:           codexGatewayModelSupportsToolCalls(model),
		ImageInput:          codexGatewayStringSliceContains(model.InputModalities, "image"),
		CachePricing:        model.Pricing != nil && (model.Pricing.CachedInputPrice != nil || model.Pricing.CacheWritePrice != nil),
		ContextContinuation: true,
	}
}

func codexGatewayModelSupportsToolCalls(model CodexGatewayModel) bool {
	for _, tool := range model.ExperimentalSupportedTools {
		switch strings.TrimSpace(tool) {
		case CodexGatewayToolKindFunction, CodexGatewayToolKindNamespace, CodexGatewayToolKindCustom:
			return true
		}
	}
	return model.SupportsParallelToolCalls
}

func codexGatewayStringSliceContains(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}

func codexGatewayResolvedPricingToCatalog(resolved *ResolvedPricing) *CodexGatewayModelPricing {
	if resolved == nil || resolved.Source != PricingSourceChannel {
		return nil
	}
	pricing := &CodexGatewayModelPricing{
		Currency: "USD",
		Unit:     "per_1m_tokens",
		Source:   "database_model_pricing",
	}
	if resolved.BasePricing != nil {
		pricing.InputPrice = codexGatewayPerMillionPrice(resolved.BasePricing.InputPricePerToken)
		pricing.OutputPrice = codexGatewayPerMillionPrice(resolved.BasePricing.OutputPricePerToken)
		pricing.CachedInputPrice = codexGatewayPerMillionPrice(resolved.BasePricing.CacheReadPricePerToken)
		pricing.CacheWritePrice = codexGatewayPerMillionPrice(firstPositiveFloat(
			resolved.BasePricing.CacheCreationPricePerToken,
			resolved.BasePricing.CacheCreation5mPrice,
			resolved.BasePricing.CacheCreation1hPrice,
		))
	}
	if len(resolved.Intervals) > 0 {
		interval := resolved.Intervals[0]
		if interval.InputPrice != nil {
			pricing.InputPrice = codexGatewayPerMillionPrice(*interval.InputPrice)
		}
		if interval.OutputPrice != nil {
			pricing.OutputPrice = codexGatewayPerMillionPrice(*interval.OutputPrice)
		}
		if interval.CacheReadPrice != nil {
			pricing.CachedInputPrice = codexGatewayPerMillionPrice(*interval.CacheReadPrice)
		}
		if interval.CacheWritePrice != nil {
			pricing.CacheWritePrice = codexGatewayPerMillionPrice(*interval.CacheWritePrice)
		}
	}
	if pricing.InputPrice == nil && pricing.OutputPrice == nil && pricing.CachedInputPrice == nil && pricing.CacheWritePrice == nil {
		return nil
	}
	return pricing
}

func codexGatewayPerMillionPrice(perToken float64) *string {
	if perToken <= 0 {
		return nil
	}
	value := strconv.FormatFloat(perToken*1_000_000, 'f', -1, 64)
	return &value
}

func codexGatewayModelToCodexCLIModel(model CodexGatewayModel) CodexGatewayCodexCLIModel {
	contextWindow := model.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 200000
	}
	maxContextWindow := contextWindow
	truncationLimit := model.AutoCompactTokenLimit
	if truncationLimit <= 0 {
		truncationLimit = int(float64(contextWindow) * 0.75)
	}
	effectiveContextWindowPercent := model.EffectiveContextWindowPercent
	if effectiveContextWindowPercent <= 0 {
		effectiveContextWindowPercent = 95
	}
	description := model.DisplayName + " via Sub2API Codex Gateway."
	if normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider)) == CodexGatewayProviderOpenAI {
		description = model.DisplayName + " via the configured OpenAI Responses upstream."
	}
	baseInstructions := codexGatewayBaseInstructionsForModel(model)
	cli := CodexGatewayCodexCLIModel{
		Slug:                          model.Slug,
		DisplayName:                   model.DisplayName,
		Origin:                        model.Origin,
		ProviderID:                    model.ProviderID,
		Capabilities:                  model.Capabilities,
		Pricing:                       model.Pricing,
		Description:                   description,
		DefaultReasoningLevel:         model.DefaultReasoningLevel,
		SupportedReasoningLevels:      codexGatewayReasoningLevelInfo(model.SupportedReasoningLevels),
		ShellType:                     codexGatewayCLIShellType(model.ShellType),
		Visibility:                    codexGatewayCLIVisibility(model.Visibility),
		SupportedInAPI:                model.SupportedInAPI,
		Priority:                      model.Priority,
		BaseInstructions:              baseInstructions,
		ModelMessages:                 codexGatewayCLIModelMessages(baseInstructions),
		ContextWindow:                 contextWindow,
		AutoCompactTokenLimit:         truncationLimit,
		MaxContextWindow:              maxContextWindow,
		EffectiveContextWindowPercent: effectiveContextWindowPercent,
		MaxOutputTokens:               model.MaxOutputTokens,
		SupportVerbosity:              model.SupportVerbosity,
		DefaultVerbosity:              codexGatewayDefaultVerbosity(model),
		ApplyPatchToolType:            "freeform",
		TruncationPolicy:              CodexGatewayCodexCLITruncation{Mode: "tokens", Limit: min(truncationLimit, 10000)},
		SupportsParallelToolCalls:     model.SupportsParallelToolCalls,
		SupportsImageDetailOriginal:   model.SupportsImageDetailOriginal,
		SupportsReasoningSummaries:    true,
		DefaultReasoningSummary:       "none",
		ExperimentalSupportedTools:    append([]string(nil), model.ExperimentalSupportedTools...),
		InputModalities:               append([]string(nil), model.InputModalities...),
		SupportsSearchTool:            model.SupportsSearchTool,
	}
	if cli.DefaultReasoningLevel == "" {
		cli.DefaultReasoningLevel = "medium"
	}
	if len(cli.SupportedReasoningLevels) == 0 {
		cli.SupportedReasoningLevels = codexGatewayReasoningLevelInfo([]string{"medium"})
	}
	if len(cli.InputModalities) == 0 {
		cli.InputModalities = []string{"text"}
	}
	if model.SupportsSearchTool {
		cli.WebSearchToolType = codexGatewayCLIWebSearchToolType(model)
	}
	return cli
}

func codexGatewayCLIModelMessages(instructions string) CodexGatewayCodexCLIModelMessages {
	return CodexGatewayCodexCLIModelMessages{
		InstructionsTemplate:  instructions,
		InstructionsVariables: map[string]string{},
	}
}

func codexGatewayCLIShellType(shellType string) string {
	switch strings.TrimSpace(shellType) {
	case "local":
		return "local"
	case "shell_command":
		return "shell_command"
	default:
		return "local"
	}
}

func codexGatewayCLIVisibility(visibility string) string {
	switch strings.TrimSpace(visibility) {
	case "hidden", "hide":
		return "hide"
	case "none":
		return "none"
	default:
		return "list"
	}
}

func codexGatewayCLIWebSearchToolType(model CodexGatewayModel) string {
	switch strings.TrimSpace(model.WebSearchToolType) {
	case "text", "text_and_image":
		return strings.TrimSpace(model.WebSearchToolType)
	default:
		if codexGatewayStringSliceContains(model.InputModalities, "image") {
			return "text_and_image"
		}
		if model.SupportsImageDetailOriginal {
			return "text_and_image"
		}
		return "text"
	}
}

func codexGatewayDefaultVerbosity(model CodexGatewayModel) string {
	if model.SupportVerbosity {
		return "low"
	}
	return ""
}

func codexGatewayReasoningLevelInfo(levels []string) []CodexGatewayReasoningLevelInfo {
	out := make([]CodexGatewayReasoningLevelInfo, 0, len(levels))
	for _, level := range levels {
		level = strings.TrimSpace(level)
		if level == "" {
			continue
		}
		out = append(out, CodexGatewayReasoningLevelInfo{
			Effort:      level,
			Description: codexGatewayReasoningLevelDescription(level),
		})
	}
	return out
}

func codexGatewayReasoningLevelDescription(level string) string {
	switch level {
	case "minimal":
		return "Fastest responses with minimal reasoning"
	case "low":
		return "Fast responses with lighter reasoning"
	case "medium":
		return "Balances speed and reasoning depth for everyday tasks"
	case "high":
		return "Greater reasoning depth for complex problems"
	case "xhigh":
		return "Extra high reasoning depth for complex problems"
	default:
		return "Reasoning effort " + level
	}
}

func codexGatewayDisplayNameWithSource(baseName, sourceName string) string {
	baseName = strings.TrimSpace(baseName)
	sourceName = strings.TrimSpace(sourceName)
	if baseName == "" || sourceName == "" {
		return baseName
	}
	return baseName + " " + sourceName
}

func codexGatewayAnthropicDisplayName(baseName, providerVariant string) string {
	baseName = strings.TrimSpace(baseName)
	switch strings.TrimSpace(providerVariant) {
	case "kiro_claude":
		return codexGatewayDisplayNameWithSource(baseName, "Kiro")
	case "kiro_claude_thinking":
		return codexGatewayDisplayNameWithSource(baseName, "Thinking Kiro")
	case "antigravity_claude":
		return codexGatewayDisplayNameWithSource(baseName, "AG")
	case "antigravity_claude_thinking":
		return codexGatewayDisplayNameWithSource(baseName, "Thinking AG")
	case "claude_code_max":
		return codexGatewayDisplayNameWithSource(baseName, "Max")
	default:
		return baseName
	}
}

func defaultCodexGatewayModels() []CodexGatewayModel {
	openAIModel := func(slug, displayName string, priority int) CodexGatewayModel {
		contextWindow := 400000
		autoCompactTokenLimit := 300000
		effectiveContextWindowPercent := 95
		switch slug {
		case "gpt-5.5":
			contextWindow = 272_000
			autoCompactTokenLimit = 244_800
			effectiveContextWindowPercent = 95
		case "gpt-5.4":
			contextWindow = 1_050_000
			autoCompactTokenLimit = 900_000
			effectiveContextWindowPercent = 92
		case "gpt-5.4-mini":
			contextWindow = 400_000
			autoCompactTokenLimit = 300_000
		}
		return CodexGatewayModel{
			Slug:                          slug,
			DisplayName:                   displayName,
			Provider:                      "openai",
			UpstreamModel:                 slug,
			Visibility:                    "visible",
			SupportedInAPI:                true,
			Priority:                      priority,
			DefaultReasoningLevel:         "medium",
			SupportedReasoningLevels:      []string{"low", "medium", "high", "xhigh"},
			SupportVerbosity:              true,
			SupportsParallelToolCalls:     true,
			ContextWindow:                 contextWindow,
			AutoCompactTokenLimit:         autoCompactTokenLimit,
			EffectiveContextWindowPercent: effectiveContextWindowPercent,
			MaxOutputTokens:               128000,
			InputModalities:               []string{"text", "image"},
			SupportsImageDetailOriginal:   true,
			SupportsSearchTool:            true,
			ExperimentalSupportedTools:    []string{"function", "namespace", "custom"},
			ShellType:                     "local",
			WebSearchToolType:             "openai",
			ImageGenerationToolType:       "openai",
		}
	}

	deepSeekModel := func(slug, displayName string, priority int) CodexGatewayModel {
		return CodexGatewayModel{
			Slug:                        slug,
			DisplayName:                 displayName,
			Provider:                    "deepseek",
			UpstreamModel:               slug,
			Visibility:                  "hidden",
			SupportedInAPI:              false,
			Priority:                    priority,
			DefaultReasoningLevel:       "xhigh",
			SupportedReasoningLevels:    []string{"high", "xhigh"},
			SupportVerbosity:            false,
			SupportsParallelToolCalls:   false,
			ContextWindow:               1_000_000,
			AutoCompactTokenLimit:       850_000,
			MaxOutputTokens:             384_000,
			InputModalities:             []string{"text", "image"},
			SupportsImageDetailOriginal: false,
			SupportsSearchTool:          true,
			ExperimentalSupportedTools:  []string{"function", "namespace", "custom"},
			ShellType:                   "local",
			WebSearchToolType:           "openai",
			ImageGenerationToolType:     "none",
		}
	}

	anthropicModel := func(slug, baseDisplayName, providerVariant, upstreamModel, defaultReasoning string, supportedReasoning []string, priority int) CodexGatewayModel {
		upstreamModel = strings.TrimSpace(upstreamModel)
		if upstreamModel == "" {
			upstreamModel = strings.TrimSpace(slug)
		}
		return CodexGatewayModel{
			Slug:                          slug,
			DisplayName:                   codexGatewayAnthropicDisplayName(baseDisplayName, providerVariant),
			Provider:                      "anthropic",
			ProviderVariant:               providerVariant,
			UpstreamModel:                 upstreamModel,
			UpstreamBaseModel:             upstreamModel,
			UpstreamThinkingModel:         upstreamModel,
			Visibility:                    "visible",
			SupportedInAPI:                true,
			Priority:                      priority,
			DefaultReasoningLevel:         defaultReasoning,
			SupportedReasoningLevels:      append([]string(nil), supportedReasoning...),
			SupportVerbosity:              false,
			SupportsParallelToolCalls:     true,
			ContextWindow:                 1_000_000,
			AutoCompactTokenLimit:         850_000,
			EffectiveContextWindowPercent: 95,
			MaxOutputTokens:               64_000,
			InputModalities:               []string{"text", "image"},
			SupportsImageDetailOriginal:   true,
			SupportsSearchTool:            true,
			ExperimentalSupportedTools:    []string{"function", "namespace", "custom"},
			ShellType:                     "local",
			WebSearchToolType:             "openai",
			ImageGenerationToolType:       "none",
		}
	}

	return []CodexGatewayModel{
		openAIModel("gpt-5.5", "GPT-5.5", 100),
		openAIModel("gpt-5.4", "GPT-5.4", 90),
		openAIModel("gpt-5.4-mini", "GPT-5.4 Mini", 80),
		openAIModel("gpt-5.3-codex", "GPT-5.3 Codex", 70),
		deepSeekModel("deepseek-v4-pro", "DeepSeek V4 Pro", 60),
		deepSeekModel("deepseek-v4-flash", "DeepSeek V4 Flash", 50),
		anthropicModel("claude-opus-4-7", "Claude Opus 4.7", "kiro_claude", "claude-opus-4-7", "high", []string{"high"}, 49),
		anthropicModel("claude-opus-4-7-thinking", "Claude Opus 4.7", "kiro_claude_thinking", "claude-opus-4-7-thinking", "high", []string{"low", "high", "xhigh"}, 48),
		anthropicModel("claude-opus-4-7-ag", "Claude Opus 4.7", "antigravity_claude", "claude-opus-4-7", "high", []string{"high"}, 47),
		anthropicModel("claude-opus-4-7-thinking-ag", "Claude Opus 4.7", "antigravity_claude_thinking", "claude-opus-4-7-thinking", "high", []string{"low", "high", "xhigh"}, 46),
		anthropicModel("claude-opus-4-7-max", "Claude Opus 4.7", "claude_code_max", "claude-opus-4-7-thinking", "xhigh", []string{"xhigh"}, 45),
		anthropicModel("claude-opus-4-6", "Claude Opus 4.6", "kiro_claude", "claude-opus-4-6", "high", []string{"high"}, 44),
		anthropicModel("claude-opus-4-6-thinking", "Claude Opus 4.6", "kiro_claude_thinking", "claude-opus-4-6-thinking", "high", []string{"low", "high", "xhigh"}, 43),
		anthropicModel("claude-opus-4-6-ag", "Claude Opus 4.6", "antigravity_claude", "claude-opus-4-6", "high", []string{"high"}, 42),
		anthropicModel("claude-opus-4-6-thinking-ag", "Claude Opus 4.6", "antigravity_claude_thinking", "claude-opus-4-6-thinking", "high", []string{"low", "high", "xhigh"}, 41),
		anthropicModel("claude-opus-4-6-max", "Claude Opus 4.6", "claude_code_max", "claude-opus-4-6-thinking", "xhigh", []string{"xhigh"}, 40),
		anthropicModel("claude-sonnet-4-6", "Claude Sonnet 4.6", "kiro_claude", "claude-sonnet-4-6", "high", []string{"high"}, 39),
		anthropicModel("claude-sonnet-4-6-thinking", "Claude Sonnet 4.6", "kiro_claude_thinking", "claude-sonnet-4-6-thinking", "high", []string{"low", "high", "xhigh"}, 38),
		anthropicModel("claude-sonnet-4-6-ag", "Claude Sonnet 4.6", "antigravity_claude", "claude-sonnet-4-6", "high", []string{"high"}, 37),
		anthropicModel("claude-sonnet-4-6-thinking-ag", "Claude Sonnet 4.6", "antigravity_claude_thinking", "claude-sonnet-4-6-thinking", "high", []string{"low", "high", "xhigh"}, 36),
		anthropicModel("claude-sonnet-4-6-max", "Claude Sonnet 4.6", "claude_code_max", "claude-sonnet-4-6-thinking", "xhigh", []string{"xhigh"}, 35),
		anthropicModel("claude-haiku-4-5-20251001", "Claude Haiku 4.5", "kiro_claude", "claude-haiku-4-5-20251001", "high", []string{"high"}, 34),
		anthropicModel("claude-haiku-4-5-20251001-thinking", "Claude Haiku 4.5", "kiro_claude_thinking", "claude-haiku-4-5-20251001-thinking", "high", []string{"low", "high", "xhigh"}, 33),
		anthropicModel("claude-haiku-4-5-20251001-ag", "Claude Haiku 4.5", "antigravity_claude", "claude-haiku-4-5-20251001", "high", []string{"high"}, 32),
		anthropicModel("claude-haiku-4-5-20251001-thinking-ag", "Claude Haiku 4.5", "antigravity_claude_thinking", "claude-haiku-4-5-20251001-thinking", "high", []string{"low", "high", "xhigh"}, 31),
		anthropicModel("claude-haiku-4-5-20251001-max", "Claude Haiku 4.5", "claude_code_max", "claude-haiku-4-5-20251001-thinking", "xhigh", []string{"xhigh"}, 30),
	}
}

func codexGatewayApplyVisibilityGates(model CodexGatewayModel, mutation CodexGatewayModelMutation, providerRuntime CodexGatewayProviderRuntime, pricingChecker CodexGatewayPricingReadyChecker, protocolChecker CodexGatewayProtocolReadyChecker, variantChecker CodexGatewayVariantReadyChecker) CodexGatewayModel {
	out := model
	if !mutation.Enabled || codexGatewayModelExplicitlyHidden(mutation) {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	if providerRuntime.GroupID <= 0 || !providerRuntime.Healthy {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	if variantChecker != nil && !variantChecker.IsReady(context.Background(), out, providerRuntime) {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	if !codexGatewayIsDeepSeekModel(out) {
		return out
	}
	if pricingChecker == nil || !pricingChecker.HasPricing(out.Slug) {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	if protocolChecker == nil || !protocolChecker.IsReady(out.Slug) {
		out.Visibility = "hidden"
		out.SupportedInAPI = false
		return out
	}
	out.Visibility = "visible"
	out.SupportedInAPI = true
	return out
}

func codexGatewayModelExplicitlyHidden(mutation CodexGatewayModelMutation) bool {
	return normalizeCodexGatewaySmokeStatus(mutation.SmokeStatus) == CodexGatewaySmokeStatusFailed
}

func codexGatewayIsDeepSeekModel(model CodexGatewayModel) bool {
	return normalizeCodexGatewayProvider(CodexGatewayProvider(model.Provider)) == CodexGatewayProviderDeepSeek
}

func mergeCodexGatewayProviderGroups(base, override map[CodexGatewayProvider]CodexGatewayProviderRuntime) map[CodexGatewayProvider]CodexGatewayProviderRuntime {
	out := make(map[CodexGatewayProvider]CodexGatewayProviderRuntime, len(base)+len(override))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func mergeCodexGatewayModelMutations(base, override map[string]CodexGatewayModelMutation) map[string]CodexGatewayModelMutation {
	out := make(map[string]CodexGatewayModelMutation, len(base)+len(override))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}
