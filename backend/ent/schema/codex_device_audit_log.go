package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CodexDeviceAuditLog holds the schema definition for the CodexDeviceAuditLog entity.
type CodexDeviceAuditLog struct {
	ent.Schema
}

func (CodexDeviceAuditLog) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "codex_device_audit_logs"},
	}
}

func (CodexDeviceAuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("device_id"),
		field.Int64("user_id"),
		field.String("event").
			MaxLen(64).
			NotEmpty(),
		field.String("ip").
			MaxLen(64).
			Default(""),
		field.String("user_agent").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.JSON("metadata", map[string]any{}).
			Default(func() map[string]any { return map[string]any{} }),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CodexDeviceAuditLog) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", User.Type).
			Unique().
			Required().
			Field("user_id"),
		edge.From("device", CodexManagedDevice.Type).
			Ref("audit_logs").
			Field("device_id").
			Unique().
			Required(),
	}
}

func (CodexDeviceAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id"),
		index.Fields("user_id"),
		index.Fields("event"),
		index.Fields("created_at"),
	}
}
