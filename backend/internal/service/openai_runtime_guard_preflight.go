package service

import "github.com/gin-gonic/gin"

func (s *OpenAIGatewayService) applyOpenAIOAuthRuntimeGuardPreflightToHTTP(
	c *gin.Context,
	account *Account,
	model string,
	endpoint string,
	protocol string,
	body []byte,
	compact bool,
) ([]byte, error) {
	if !shouldApplyOpenAIReasoningEffortGuard(account) {
		return body, nil
	}
	if blocked := s.blockOpenAIRuntimeGuardLearnedRequest(c, account, model, endpoint); blocked != nil {
		return body, blocked
	}
	repairedBody, shapeBlocked, shapeErr := applyOpenAIRuntimeGuardShapeGuardToBody(body)
	if shapeErr != nil {
		return body, shapeErr
	}
	if shapeBlocked != nil {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		if c != nil {
			c.Data(shapeBlocked.StatusCode, "application/json; charset=utf-8", shapeBlocked.Payload)
		}
		return body, shapeBlocked
	}
	body = repairedBody

	reasoningDecision := evaluateOpenAIReasoningEffortGuard(body)
	if reasoningDecision.Blocked {
		setOpenAIRuntimeGuardReasoningMetadata(c, reasoningDecision)
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		writeOpenAIReasoningEffortGuardBlockedResponse(c, reasoningDecision)
		return body, newOpenAIRuntimeGuardBlockedError(reasoningDecision)
	}
	if reasoningDecision.Repaired {
		var repairErr error
		body, repairErr = applyOpenAIReasoningEffortGuardRepairs(body, reasoningDecision)
		if repairErr != nil {
			return body, repairErr
		}
	}
	setOpenAIRuntimeGuardReasoningMetadata(c, reasoningDecision)

	if blocked := s.applyOpenAIRuntimeGuardContentSafetyToHTTP(c, account, protocol, body); blocked != nil {
		return body, blocked
	}
	if personaDecision := evaluateOpenAIOAuthCodexPersonaGuard(account, model, ""); personaDecision.Blocked {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		if c != nil {
			c.Data(openAIReasoningEffortGuardBlockedStatus(personaDecision), "application/json; charset=utf-8", openAIReasoningEffortGuardBlockedPayload(personaDecision))
		}
		return body, newOpenAIRuntimeGuardBlockedError(personaDecision)
	}
	if blocked := applyOpenAIRuntimeGuardContextToHTTP(c, account, model, body, compact); blocked != nil {
		return body, blocked
	}
	return body, nil
}
