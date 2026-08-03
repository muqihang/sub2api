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

// CodexSetupGrant holds the schema definition for the CodexSetupGrant entity.
type CodexSetupGrant struct {
	ent.Schema
}

func (CodexSetupGrant) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "codex_setup_grants"},
	}
}

func (CodexSetupGrant) Fields() []ent.Field {
	return []ent.Field{
		field.String("code_hash").
			MaxLen(128).
			NotEmpty().
			Unique(),
		field.Int64("user_id"),
		field.Int64("api_key_id"),
		field.String("mode").
			MaxLen(32).
			NotEmpty(),
		field.String("server_origin").
			MaxLen(255).
			NotEmpty(),
		field.String("gateway_origin").
			MaxLen(255).
			NotEmpty(),
		field.Time("expires_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("consumed_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CodexSetupGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("user", User.Type).
			Unique().
			Required().
			Field("user_id"),
		edge.To("api_key", APIKey.Type).
			Unique().
			Required().
			Field("api_key_id"),
	}
}

func (CodexSetupGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code_hash"),
		index.Fields("user_id"),
		index.Fields("api_key_id"),
		index.Fields("expires_at"),
	}
}
