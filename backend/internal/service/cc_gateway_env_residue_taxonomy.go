package service

import (
	"net/url"
	"strings"
)

const ccGatewayVerifiedEnvResidueCNTLD = "cn"

var ccGatewayVerifiedEnvResidueExactDomains = []string{
	"sankuai.com", "netease.com", "163.com", "baidu-int.com",
	"baidu.com", "alibaba-inc.com", "alipay.com", "antgroup-inc.cn",
	"kuaishou.com", "bytedance.net", "xiaohongshu.com", "ctripcorp.com",
	"jd.com", "jdcloud.com", "bilibili.co", "iflytek.com",
	"stepfun-inc.com", "aliyuncs.com", "cn-shanghai.fcapp.run", "cn-beijing.fcapp.run",
	"xaminim.com", "moonshot.ai", "anyrouter.top", "packyapi.com",
	"aicodemirror.com", "aigocode.com", "hongshan.com", "iwhalecloud.com",
	"dhcoder.net", "lemongpt.top", "zhihuiapi.top", "intsig.net",
	"high-five-ai.xyz", "cloudsway.net", "4sapi.com", "529961.com",
	"88996.cloud", "88code.ai", "88code.org", "91code.pro",
	"992236.xyz", "ai.codeqaq.com", "ai.hybgzs.com", "ai.kjvhh.com",
	"aicanapi.com", "aicoding.sh", "aifast.site", "aihubmix.com",
	"anmory.com", "api.5202030.xyz", "api.ablai.top", "api.bianxie.ai",
	"api.bltcy.ai", "api.cpass.cc", "api.dev88.tech", "api.dreamger.com",
	"api.expansion.chat", "api.gueai.com", "api.holdai.top", "api.ikuncode.cc",
	"api.lconai.com", "api.linkapi.org", "api.mkeai.com", "api.nekoapi.com",
	"api.oaipro.com", "api.ruyun.fun", "api.ssopen.top", "api.tu-zi.com",
	"api.uglycat.cc", "api.v3.cm", "api.whatai.cc", "api.wpgzs.top",
	"api.xty.app", "api.yuegle.com", "api.zzyu.me", "apimart.ai",
	"apipro.maynor1024.live", "apiyi.com", "applyj.hiapi.top", "augmunt.com",
	"b4u.qzz.io", "clauddy.com", "claude-code-hub.app", "claude-opus.top",
	"claudeide.net", "co.yes.vg", "code.wenwen-ai.com", "code.x-aio.com",
	"codeilab.com", "cubence.com", "deeprouter.top", "dimaray.com",
	"dmxapi.com", "docs.aigc2d.com", "duckcoding.com", "fk.hshwk.org",
	"flapcode.com", "foxcode.hshwk.org", "foxcode.rjj.cc", "fuli.hxi.me",
	"getgoapi.com", "gpt.zhizengzeng.com", "gptgod.cloud", "gptkey.eu.org",
	"gptpay.store", "hdgsb.com", "henapi.top", "instcopilot-api.com",
	"jeniya.top", "jiekou.ai", "kg-api.cloud", "n1n.ai",
	"new-api.u4vr.com", "new.xychatai.com", "one-api.bltcy.top", "one.ocoolai.com",
	"oneapi.paintbot.top", "open.xiaojingai.com", "openclaude.me", "opus.gptuu.com",
	"poloai.top", "poloapi.top", "privnode.com", "proxyai.com",
	"qinzhiai.com", "right.codes", "runanytime.hxi.me", "sssaicode.com",
	"store.zzyus.top", "tiantianai.pro", "uiuiapi.com", "uniapi.ai",
	"vip.undyingapi.com", "wolfai.top", "wzw.de5.net", "wzw.pp.ua",
	"xairouter.com", "xaixapi.com", "xiaohuapi.site", "xiaohumini.site",
	"xy.poloapi.com", "yansd666.com", "yansd666.top", "yunwu.ai",
	"yunwu.zeabur.app", "zenmux.ai",
}
var ccGatewayVerifiedEnvResidueKeywords = []string{
	"deepseek", "moonshot", "minimax", "xaminim",
	"zhipu", "bigmodel", "baichuan", "stepfun",
	"01ai", "dashscope", "volces",
}

func ccGatewayClassifyVerifiedEnvResidueBaseURL(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	host := ccGatewayEnvResidueHost(value)
	matchTarget := host
	if matchTarget == "" {
		matchTarget = value
	}

	if ccGatewayEnvResidueIsOfficialAnthropicHost(host) || strings.Contains(value, "api.anthropic.com") || strings.Contains(value, "anthropic.com") {
		return "official_anthropic"
	}
	if ccGatewayEnvResidueIsNeutralGateway(matchTarget, value) {
		return "neutral_gateway"
	}

	exact := ccGatewayEnvResidueMatchesExactDomainList(matchTarget)
	keyword := ccGatewayEnvResidueMatchesKeyword(matchTarget)
	switch {
	case exact && keyword:
		return "exact_domain_and_keyword"
	case exact:
		return "exact_domain_list"
	case keyword:
		return "keyword"
	}

	if matchTarget == ccGatewayVerifiedEnvResidueCNTLD || strings.HasSuffix(matchTarget, "."+ccGatewayVerifiedEnvResidueCNTLD) {
		return "cn_tld"
	}
	if strings.Contains(value, ".cn") {
		return "china_tld"
	}
	if strings.HasSuffix(matchTarget, ".org") || strings.Contains(value, ".org") {
		return "china_org_domain"
	}
	if strings.Contains(matchTarget, "cloud") {
		return "china_cloud_domain"
	}
	if ccGatewayEnvResidueHasAILabBoundary(matchTarget) {
		return "ai_lab_keyword"
	}
	if ccGatewayEnvResidueContainsAny(matchTarget, []string{"proxy", "resale"}) {
		return "claude_proxy_resale_like"
	}
	return "unknown"
}

func ccGatewayEnvResidueHost(value string) string {
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	}
	if parsed, err := url.Parse("//" + value); err == nil && parsed.Hostname() != "" {
		return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	}
	host := value
	if before, _, ok := strings.Cut(host, "/"); ok {
		host = before
	}
	if before, _, ok := strings.Cut(host, "?"); ok {
		host = before
	}
	if before, _, ok := strings.Cut(host, "#"); ok {
		host = before
	}
	if _, after, ok := strings.Cut(host, "@"); ok {
		host = after
	}
	host = strings.Trim(host, "[] ")
	if before, _, ok := strings.Cut(host, ":"); ok {
		host = before
	}
	return strings.TrimSuffix(host, ".")
}

func ccGatewayEnvResidueIsOfficialAnthropicHost(host string) bool {
	return host == "anthropic.com" || host == "api.anthropic.com" || strings.HasSuffix(host, ".anthropic.com")
}

func ccGatewayEnvResidueMatchesExactDomainList(host string) bool {
	for _, domain := range ccGatewayVerifiedEnvResidueExactDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func ccGatewayEnvResidueMatchesKeyword(host string) bool {
	for _, keyword := range ccGatewayVerifiedEnvResidueKeywords {
		if strings.Contains(host, keyword) {
			return true
		}
	}
	return false
}

func ccGatewayEnvResidueIsNeutralGateway(host string, value string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		host == "0.0.0.0" ||
		host == "test.invalid" || strings.HasSuffix(host, ".test.invalid") ||
		strings.Contains(host, "gateway") || strings.Contains(value, "localhost") || strings.Contains(value, "127.0.0.1")
}

func ccGatewayEnvResidueHasAILabBoundary(value string) bool {
	if strings.HasSuffix(value, ".ai") {
		return true
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == ':' || r == '-'
	})
	for _, part := range parts {
		if part == "ai" || part == "lab" {
			return true
		}
	}
	return false
}

func ccGatewayEnvResidueContainsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
