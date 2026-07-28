//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSMTPMessageAddsNamedMailboxHeaders(t *testing.T) {
	config := &SMTPConfig{
		From:     "verification@5566676.xyz",
		FromName: "逐梦验证码",
	}

	message, envelopeTo := buildSMTPMessage(
		config,
		"369492318@qq.com",
		"[逐梦] 邮箱验证码",
		"<p>test</p>",
	)

	require.Equal(t, "369492318@qq.com", envelopeTo)
	require.Contains(t, strings.ToLower(message), "from: =?utf-8?")
	require.Contains(t, message, "<verification@5566676.xyz>\r\n")
	require.Contains(t, strings.ToLower(message), "sender: =?utf-8?")
	require.Contains(t, message, "To: \"369492318\" <369492318@qq.com>\r\n")
	require.Contains(t, message, "Subject: =?UTF-8?")
	require.True(t, strings.HasSuffix(message, "\r\n\r\n<p>test</p>"))
}

func TestBuildSMTPMessageStripsHeaderInjection(t *testing.T) {
	config := &SMTPConfig{
		From:     "verification@example.com\r\nBcc: attacker@example.com",
		FromName: "Sender\r\nBcc: attacker@example.com",
	}

	message, envelopeTo := buildSMTPMessage(
		config,
		"recipient@example.com\r\nBcc: attacker@example.com",
		"Subject\r\nBcc: attacker@example.com",
		"<p>test</p>",
	)

	require.NotContains(t, message, "\r\nBcc:")
	require.NotContains(t, envelopeTo, "\r")
	require.NotContains(t, envelopeTo, "\n")
}
