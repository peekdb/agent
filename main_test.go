package main

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		limit    int
		expected string
	}{
		{
			name:     "short string unchanged",
			input:    "hello",
			limit:    10,
			expected: "hello",
		},
		{
			name:     "exact length unchanged",
			input:    "hello",
			limit:    5,
			expected: "hello",
		},
		{
			name:     "long string truncated",
			input:    "hello world",
			limit:    5,
			expected: "hello...",
		},
		{
			name:     "empty string",
			input:    "",
			limit:    10,
			expected: "",
		},
		{
			name:     "single character over limit",
			input:    "ab",
			limit:    1,
			expected: "a...",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncate(tc.input, tc.limit)
			if result != tc.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.limit, result, tc.expected)
			}
		})
	}
}

func TestExecuteQuery(t *testing.T) {
	tests := []struct {
		name           string
		queryID        string
		sql            string
		params         []any
		mockSetup      func(sqlmock.Sqlmock)
		expectedCols   []string
		expectedRows   int
		expectedError  string
	}{
		{
			name:    "simple SELECT",
			queryID: "q1",
			sql:     "SELECT id FROM users WHERE id = $1",
			params:  []any{1},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"id"}).AddRow(1)
				mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1").
					WithArgs(1).
					WillReturnRows(rows)
			},
			expectedCols: []string{"id"},
			expectedRows: 1,
		},
		{
			name:    "multiple columns",
			queryID: "q2",
			sql:     "SELECT id, name, email FROM users",
			params:  nil,
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"id", "name", "email"}).
					AddRow(1, "Alice", "alice@example.com").
					AddRow(2, "Bob", "bob@example.com")
				mock.ExpectQuery("SELECT id, name, email FROM users").
					WillReturnRows(rows)
			},
			expectedCols: []string{"id", "name", "email"},
			expectedRows: 2,
		},
		{
			name:    "empty result",
			queryID: "q3",
			sql:     "SELECT * FROM users WHERE id = $1",
			params:  []any{999},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"id", "name"})
				mock.ExpectQuery("SELECT \\* FROM users WHERE id = \\$1").
					WithArgs(999).
					WillReturnRows(rows)
			},
			expectedCols: []string{"id", "name"},
			expectedRows: 0,
		},
		{
			name:    "SQL error",
			queryID: "q4",
			sql:     "SELECT * FROM nonexistent_table",
			params:  nil,
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT \\* FROM nonexistent_table").
					WillReturnError(sqlmock.ErrCancelled)
			},
			expectedError: "canceling query due to user request",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock database
			mockDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer mockDB.Close()

			// Replace global db with mock
			originalDB := db
			db = mockDB
			defer func() { db = originalDB }()

			tc.mockSetup(mock)

			// Execute query
			result := executeQuery(tc.queryID, tc.sql, tc.params)

			// Verify result
			if result.ID != tc.queryID {
				t.Errorf("expected ID %q, got %q", tc.queryID, result.ID)
			}
			if result.Type != "result" {
				t.Errorf("expected Type 'result', got %q", result.Type)
			}

			if tc.expectedError != "" {
				if result.Error == "" {
					t.Errorf("expected error containing %q, got none", tc.expectedError)
				}
			} else {
				if result.Error != "" {
					t.Errorf("unexpected error: %s", result.Error)
				}
				if len(result.Columns) != len(tc.expectedCols) {
					t.Errorf("expected %d columns, got %d", len(tc.expectedCols), len(result.Columns))
				}
				for i, col := range tc.expectedCols {
					if result.Columns[i] != col {
						t.Errorf("column %d: expected %q, got %q", i, col, result.Columns[i])
					}
				}
				if len(result.Rows) != tc.expectedRows {
					t.Errorf("expected %d rows, got %d", tc.expectedRows, len(result.Rows))
				}
			}

			// Verify all expectations were met
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestExecuteQuery_TypeConversion(t *testing.T) {
	tests := []struct {
		name          string
		queryID       string
		sql           string
		mockSetup     func(sqlmock.Sqlmock)
		checkResult   func(*testing.T, QueryResponse)
	}{
		{
			name:    "[]byte to string conversion",
			queryID: "tc1",
			sql:     "SELECT data FROM binaries",
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"data"}).
					AddRow([]byte("binary data as string"))
				mock.ExpectQuery("SELECT data FROM binaries").
					WillReturnRows(rows)
			},
			checkResult: func(t *testing.T, resp QueryResponse) {
				if resp.Error != "" {
					t.Fatalf("unexpected error: %s", resp.Error)
				}
				if len(resp.Rows) != 1 || len(resp.Rows[0]) != 1 {
					t.Fatalf("expected 1 row with 1 column, got %d rows", len(resp.Rows))
				}
				val, ok := resp.Rows[0][0].(string)
				if !ok {
					t.Errorf("expected string, got %T", resp.Rows[0][0])
				}
				if val != "binary data as string" {
					t.Errorf("expected 'binary data as string', got %q", val)
				}
			},
		},
		{
			name:    "time.Time to RFC3339 conversion",
			queryID: "tc2",
			sql:     "SELECT created_at FROM events",
			mockSetup: func(mock sqlmock.Sqlmock) {
				testTime := time.Date(2025, 2, 13, 14, 30, 0, 0, time.UTC)
				rows := sqlmock.NewRows([]string{"created_at"}).
					AddRow(testTime)
				mock.ExpectQuery("SELECT created_at FROM events").
					WillReturnRows(rows)
			},
			checkResult: func(t *testing.T, resp QueryResponse) {
				if resp.Error != "" {
					t.Fatalf("unexpected error: %s", resp.Error)
				}
				if len(resp.Rows) != 1 || len(resp.Rows[0]) != 1 {
					t.Fatalf("expected 1 row with 1 column, got %d rows", len(resp.Rows))
				}
				val, ok := resp.Rows[0][0].(string)
				if !ok {
					t.Errorf("expected string (RFC3339), got %T", resp.Rows[0][0])
				}
				expected := "2025-02-13T14:30:00Z"
				if val != expected {
					t.Errorf("expected %q, got %q", expected, val)
				}
			},
		},
		{
			name:    "null handling",
			queryID: "tc3",
			sql:     "SELECT nullable_col FROM test",
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"nullable_col"}).
					AddRow(nil)
				mock.ExpectQuery("SELECT nullable_col FROM test").
					WillReturnRows(rows)
			},
			checkResult: func(t *testing.T, resp QueryResponse) {
				if resp.Error != "" {
					t.Fatalf("unexpected error: %s", resp.Error)
				}
				if len(resp.Rows) != 1 || len(resp.Rows[0]) != 1 {
					t.Fatalf("expected 1 row with 1 column, got %d rows", len(resp.Rows))
				}
				if resp.Rows[0][0] != nil {
					t.Errorf("expected nil for null value, got %v", resp.Rows[0][0])
				}
			},
		},
		{
			name:    "mixed types in one row",
			queryID: "tc4",
			sql:     "SELECT id, name, data, created_at FROM mixed",
			mockSetup: func(mock sqlmock.Sqlmock) {
				testTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
				rows := sqlmock.NewRows([]string{"id", "name", "data", "created_at"}).
					AddRow(42, "test", []byte("blob"), testTime)
				mock.ExpectQuery("SELECT id, name, data, created_at FROM mixed").
					WillReturnRows(rows)
			},
			checkResult: func(t *testing.T, resp QueryResponse) {
				if resp.Error != "" {
					t.Fatalf("unexpected error: %s", resp.Error)
				}
				if len(resp.Rows) != 1 {
					t.Fatalf("expected 1 row, got %d", len(resp.Rows))
				}
				row := resp.Rows[0]
				if len(row) != 4 {
					t.Fatalf("expected 4 columns, got %d", len(row))
				}

				// id should remain as int64
				if id, ok := row[0].(int64); !ok || id != 42 {
					t.Errorf("id: expected 42, got %v (%T)", row[0], row[0])
				}
				// name should remain as string
				if name, ok := row[1].(string); !ok || name != "test" {
					t.Errorf("name: expected 'test', got %v", row[1])
				}
				// data ([]byte) should be converted to string
				if data, ok := row[2].(string); !ok || data != "blob" {
					t.Errorf("data: expected 'blob' (string), got %v (%T)", row[2], row[2])
				}
				// created_at should be RFC3339 string
				if ts, ok := row[3].(string); !ok || ts != "2025-01-01T00:00:00Z" {
					t.Errorf("created_at: expected '2025-01-01T00:00:00Z', got %v", row[3])
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("failed to create sqlmock: %v", err)
			}
			defer mockDB.Close()

			originalDB := db
			db = mockDB
			defer func() { db = originalDB }()

			tc.mockSetup(mock)

			result := executeQuery(tc.queryID, tc.sql, nil)
			tc.checkResult(t, result)

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}
