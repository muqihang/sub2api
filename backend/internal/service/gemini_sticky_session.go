package service

import (
  "crypto/sha256"
  "encoding/hex"
  "strings"

  "github.com/gin-gonic/gin"
  "github.com/tidwall/gjson"
)

// GenerateGeminiStickySessionHash generates a sticky-session hash for Gemini v1beta requests.
// Priority:
//  1. Header: session_id
//  2. Header: conversation_id
//  3. Header: x-opencode-session
//  4. Body:   contents[0].parts[0].text
func GenerateGeminiStickySessionHash(c *gin.Context, body []byte) string {
  if c == nil {
    return ""
  }

  sessionID := strings.TrimSpace(c.GetHeader("session_id"))
  if sessionID == "" {
    sessionID = strings.TrimSpace(c.GetHeader("conversation_id"))
  }
  if sessionID == "" {
    sessionID = strings.TrimSpace(c.GetHeader("x-opencode-session"))
  }
  if sessionID == "" && len(body) > 0 {
    sessionID = strings.TrimSpace(gjson.GetBytes(body, "contents.0.parts.0.text").String())
  }
  if sessionID == "" {
    return ""
  }

  hash := sha256.Sum256([]byte(sessionID))
  return hex.EncodeToString(hash[:])
}
