# Zhumeng Service-Specific Terms

**Effective and last updated: July 23, 2026**

These terms supplement the Zhumeng Terms of Service. If they conflict, these Service-Specific Terms control for the relevant function, without reducing any upstream-provider requirement.

## 1. APIs, Models, and Routing

1. A model identifier may map to different providers, versions, account pools, or compatible interfaces. Unless an order expressly states otherwise, you are not entitled to a fixed provider, account, data center, or version.
2. We may load-balance, retry, degrade, or fail over for capacity, security, price, quality, or recovery. Routes may differ in output, latency, metering, and tool capabilities.
3. You must implement idempotency, timeouts, backoff, retry limits, and duplicate-output handling. Automated retries may incur additional charges; an HTTP status code is not the sole billing record.

## 2. Responses, Chat, Streaming, and Compact

1. Interface compatibility does not mean complete identity with any official SDK or service. Fields, event order, finish reasons, encrypted content, reasoning blocks, tool blocks, and error formats may be normalized or passed through.
2. Streams may end early due to client networks, CDNs, proxies, upstream timeouts, or route changes. Preserve required state and do not treat unverified partial output as complete.
3. Compact, context compression, and remote summarization may invoke a model and consume usage. Compression may omit, generalize, or alter details; independently preserve and verify critical instructions, credentials, and facts.

## 3. Search and Tool Calls

1. Web Search, file retrieval, code execution, and other tools may be provided by upstream or third parties with separate terms, regional restrictions, quotas, and content licenses.
2. Search results, summaries, links, and citations may be stale, incomplete, or wrong. Verify sources. You may not use the Service for large-scale scraping, unauthorized indexes or databases, access-control circumvention, or content-rights infringement.
3. Tool calls may access external resources or cause side effects. You must establish permission boundaries, confirmation steps, and human oversight. Zhumeng is not responsible for third-party actions you authorize.

## 4. API Keys, Concurrency, and Scheduling

1. API keys are for authorized accounts and applications only. Concurrency, rate, and model permissions are governed by live platform configuration and may be applied per account, key, group, model, IP, or risk level.
2. We may queue, reject, deprioritize, or suspend high concurrency, abnormal requests, excessive long-lived connections, rate-limit evasion, or traffic that harms shared capacity.
3. “Available,” “operational,” or a passing monitor describes only a point-in-time check and does not guarantee future success. Upstream quotas, weekly limits, workspace status, and authorization may change at any time.

## 5. Data, Logs, and Retention

1. For routing, metering, troubleshooting, security, and disputes, we may record request identifiers, models, token counts, status, errors, latency, IP, key identifiers, and limited content where necessary. Retention depends on operational, security, and legal needs.
2. Do not submit secrets, regulated data, or highly sensitive personal information that is not necessary. Where submission is necessary, you are responsible for authority, minimization, encryption, and selection of appropriate models and regions.
3. Third-party and upstream processing is governed by their terms. Zhumeng cannot promise on an upstream’s behalf zero retention, training exclusion, data residency, or deletion timing unless we expressly confirm it in writing.

## 6. Support, Maintenance, and Experimental Features

1. Support is provided on a commercially reasonable-efforts basis without guaranteed response or repair times unless a written SLA states otherwise.
2. Beta, experimental, preview, alias, and newly integrated features may change or be withdrawn without notice and are not suitable for critical production workloads.
3. We may schedule maintenance, restrict abnormal traffic, or urgently disable risky routes. Status pages, monitors, and support information are informational and do not promise continuous availability.

Contact: `admin@5566676.xyz`
