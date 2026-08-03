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

// CodexDeviceToken holds the schema definition for the CodexDeviceToken entity.
type CodexDeviceToken struct {
	ent.Schema
}

func (CodexDeviceToken) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "codex_device_tokens"},
	}
}

func (CodexDeviceToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("device_id"),
		field.String("refresh_token_hash").
			MaxLen(128).
			NotEmpty().
			Unique(),
		field.Time("expires_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("rotated_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("revoked_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CodexDeviceToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("device", CodexManagedDevice.Type).
			Ref("tokens").
			Field("device_id").
			Unique().
			Required(),
	}
}

func (CodexDeviceToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id"),
		index.Fields("refresh_token_hash"),
		index.Fields("expires_at"),
	}
}
