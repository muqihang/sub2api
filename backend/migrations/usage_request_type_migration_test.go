package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration173AllowsCyberBlockedUsageRequestType(t *testing.T) {
	entries, err := FS.ReadDir(".")
	require.NoError(t, err)

	previousIndex := -1
	currentIndex := -1
	for index, entry := range entries {
		switch entry.Name() {
		case "169_batch_image_parent_batch.sql":
			previousIndex = index
		case "173_allow_cyber_blocked_usage_request_type.sql":
			currentIndex = index
		}
	}
	require.NotEqual(t, -1, previousIndex)
	require.NotEqual(t, -1, currentIndex)
	require.Less(t, previousIndex, currentIndex)

	content, err := FS.ReadFile("173_allow_cyber_blocked_usage_request_type.sql")
	require.NoError(t, err)
	text := string(content)
	require.Contains(t, text, "CHECK (request_type IN (0, 1, 2, 3, 4)) NOT VALID")
}
