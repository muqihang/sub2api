package service

import (
	"encoding/json"
	"testing"
)

func TestLoginAgreementDocumentsPreserveEnglishContent(t *testing.T) {
	docs := []LoginAgreementDocument{
		{
			ID:          "terms",
			Title:       " 服务条款 ",
			ContentMD:   " 中文内容 ",
			TitleEN:     " Terms of Service ",
			ContentMDEN: " English content ",
		},
	}

	raw, err := marshalLoginAgreementDocuments(docs)
	if err != nil {
		t.Fatalf("marshalLoginAgreementDocuments() error = %v", err)
	}

	var encoded []map[string]string
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		t.Fatalf("unmarshal encoded documents: %v", err)
	}
	if got := encoded[0]["title_en"]; got != "Terms of Service" {
		t.Fatalf("title_en = %q, want %q", got, "Terms of Service")
	}
	if got := encoded[0]["content_md_en"]; got != "English content" {
		t.Fatalf("content_md_en = %q, want %q", got, "English content")
	}

	parsed := parseLoginAgreementDocuments(raw)
	if len(parsed) != 1 {
		t.Fatalf("len(parsed) = %d, want 1", len(parsed))
	}
	if parsed[0].TitleEN != "Terms of Service" || parsed[0].ContentMDEN != "English content" {
		t.Fatalf("parsed English fields = %#v", parsed[0])
	}
}

func TestLoginAgreementDocumentWithEnglishOnlyIsNotDropped(t *testing.T) {
	docs := normalizeLoginAgreementDocuments([]LoginAgreementDocument{
		{
			ID:          "english-only",
			TitleEN:     "English only",
			ContentMDEN: "English content",
		},
	})

	if len(docs) != 1 {
		t.Fatalf("len(docs) = %d, want 1", len(docs))
	}
}
