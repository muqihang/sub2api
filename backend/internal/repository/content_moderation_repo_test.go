package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildContentModerationLogWhere_BlockedIncludesAllBlockActions(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Result: "blocked"})

	require.Empty(t, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.action IN ('block', 'keyword_block', 'hash_block')")
	require.NotContains(t, sql, "l.action = 'block'")
}

func TestContentModerationRepositoryCreateLogPersistsMatchedKeyword(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	now := time.Now().UTC()
	entry := &service.ContentModerationLog{
		RequestID:         "req-1",
		UserEmail:         "user@example.com",
		Endpoint:          "/v1/messages",
		Provider:          "anthropic",
		Model:             "claude-sonnet-5",
		Mode:              service.ContentModerationModePreBlock,
		Action:            service.ContentModerationActionKeywordBlock,
		Flagged:           true,
		HighestCategory:   "keyword",
		HighestScore:      1,
		CategoryScores:    map[string]float64{"keyword": 1},
		ThresholdSnapshot: map[string]float64{},
		InputExcerpt:      "redacted excerpt",
		MatchedKeyword:    "blocked-keyword",
	}

	mock.ExpectQuery("INSERT INTO content_moderation_logs").
		WithArgs(
			entry.RequestID, nil, entry.UserEmail, nil, entry.APIKeyName, nil, entry.GroupName,
			entry.Endpoint, entry.Provider, entry.Model, entry.Mode, entry.Action, entry.Flagged, entry.HighestCategory, entry.HighestScore,
			`{"keyword":1}`, `{}`, entry.InputExcerpt, nil, entry.Error,
			entry.ViolationCount, entry.AutoBanned, entry.EmailSent, nil, entry.MatchedKeyword,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(123), now))

	require.NoError(t, repo.CreateLog(context.Background(), entry))
	require.Equal(t, int64(123), entry.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryListLogsReadsMatchedKeyword(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	now := time.Now().UTC()
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM content_moderation_logs").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	rows := sqlmock.NewRows([]string{
		"id", "request_id", "user_id", "user_email", "api_key_id", "api_key_name", "group_id", "group_name",
		"endpoint", "provider", "model", "mode", "action", "flagged", "highest_category", "highest_score",
		"category_scores", "threshold_snapshot", "input_excerpt", "upstream_latency_ms", "error",
		"violation_count", "auto_banned", "email_sent", "user_status", "queue_delay_ms", "matched_keyword", "created_at",
	}).AddRow(
		int64(1), "req-1", nil, "user@example.com", nil, "", nil, "",
		"/v1/messages", "anthropic", "claude-sonnet-5", service.ContentModerationModePreBlock, service.ContentModerationActionKeywordBlock, true, "keyword", 1.0,
		[]byte(`{"keyword":1}`), []byte(`{}`), "redacted excerpt", nil, "",
		0, false, false, "", nil, "blocked-keyword", now,
	)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	items, page, err := repo.ListLogs(context.Background(), service.ContentModerationLogFilter{})

	require.NoError(t, err)
	require.NotNil(t, page)
	require.Len(t, items, 1)
	require.Equal(t, "blocked-keyword", items[0].MatchedKeyword)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryCountFlaggedByUserSince_ExcludesHashBlock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND action <> 'hash_block'")).
		WithArgs(int64(1001), since, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	count, err := repo.CountFlaggedByUserSince(context.Background(), 1001, since, false)

	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryCountFlaggedByUserSince_ExcludesCyberPolicyWhenRequested(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND ($3::bool IS FALSE OR action <> 'cyber_policy')")).
		WithArgs(int64(1001), since, true).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	count, err := repo.CountFlaggedByUserSince(context.Background(), 1001, since, true)

	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.NoError(t, mock.ExpectationsWereMet())
}
