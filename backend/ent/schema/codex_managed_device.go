package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CodexManagedDevice holds the schema definition for the CodexManagedDevice entity.
type CodexManagedDevice struct {
	ent.Schema
}

func (CodexManagedDevice) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "codex_managed_devices"},
	}
}

func (CodexManagedDevice) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (CodexManagedDevice) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Int64("api_key_id"),
		field.String("name").
			MaxLen(128).
			NotEmpty(),
		field.String("platform").
			MaxLen(32).
			NotEmpty(),
		field.String("arch").
			MaxLen(32).
			NotEmpty(),
		field.String("manager_version").
			MaxLen(64).
			NotEmpty(),
		field.Enum("status").
			Values("active", "revoked", "reauthorization_required").
			Default("active"),
		field.Time("last_seen_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("revoked_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CodexManagedDevice) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", User.Type).
			Unique().
			Required().
			Field("user_id"),
		edge.To("api_key", APIKey.Type).
			Unique().
			Required().
			Field("api_key_id"),
		edge.To("tokens", CodexDeviceToken.Type),
		edge.To("audit_logs", CodexDeviceAuditLog.Type),
	}
}

func (CodexManagedDevice) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("api_key_id"),
		index.Fields("status"),
	}
}
