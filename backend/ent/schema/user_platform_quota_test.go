package schema_test

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/schema"
)

func TestUserPlatformQuotaPlatformValidatorAllowsGrok(t *testing.T) {
	fields := schema.UserPlatformQuota{}.Fields()
	platformDesc := fields[1].Descriptor()
	for _, validator := range platformDesc.Validators {
		fn, ok := validator.(func(string) error)
		if !ok {
			t.Fatalf("unexpected validator type %T", validator)
		}
		if err := fn("grok"); err != nil {
			t.Fatalf("grok should be accepted by user_platform_quota platform validator: %v", err)
		}
	}
}
