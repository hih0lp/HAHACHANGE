package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

//go:embed fixtures.json
var fixtureData []byte

type store struct {
	db *pgxpool.Pool
}

func (s store) seed(ctx context.Context, e embedder, now time.Time) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(380033)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, schema); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO chat_groups(id,name) VALUES
			('product','Команда продукта'), ('catalog','Каталог'), ('search','Поиск')
		ON CONFLICT DO NOTHING;
		INSERT INTO chat_group_members(group_id,user_id,display_name) VALUES
			('product','intern','Стажёр каталога'), ('product','intern2','Стажёр поиска'),
			('product','approver','Наставник'), ('catalog','intern','Стажёр каталога'),
			('catalog','approver','Наставник'), ('search','intern2','Стажёр поиска'),
			('search','approver','Наставник')
		ON CONFLICT DO NOTHING;
	`); err != nil {
		return err
	}

	var fixtures []struct {
		Key, Group, Author, Text string
		DaysAgo                  int `json:"days_ago"`
		Hour, Minute             int
	}
	if err = json.Unmarshal(fixtureData, &fixtures); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT fixture_key FROM chat_messages WHERE fixture_key IS NOT NULL`)
	if err != nil {
		return err
	}
	seeded := make(map[string]bool)
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		seeded[key] = true
	}
	if err = rows.Err(); err != nil {
		return err
	}

	texts := make([]string, 0, len(fixtures))
	for _, f := range fixtures {
		if !seeded[f.Key] {
			texts = append(texts, f.Text)
		}
	}
	if len(texts) == 0 {
		return tx.Commit(ctx)
	}
	vectors, err := e.embed(ctx, "passage", texts)
	if err != nil {
		return err
	}
	i := 0
	for _, f := range fixtures {
		if seeded[f.Key] {
			continue
		}
		date := now.AddDate(0, 0, -f.DaysAgo)
		sent := time.Date(date.Year(), date.Month(), date.Day(), f.Hour, f.Minute, 0, 0, now.Location())
		if _, err = tx.Exec(ctx, `
			INSERT INTO chat_messages(fixture_key,group_id,author_id,body,sent_at,embedding)
			VALUES ($1,$2,$3,$4,$5,$6)
		`, f.Key, f.Group, f.Author, f.Text, sent, vectors[i]); err != nil {
			return err
		}
		i++
	}
	return tx.Commit(ctx)
}

type author struct {
	groupID, group, id, name string
}

func (s store) authors(ctx context.Context) ([]author, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g.id, g.name, m.user_id, m.display_name FROM chat_groups g
		JOIN chat_group_members m ON m.group_id = g.id ORDER BY g.name, m.display_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var authors []author
	for rows.Next() {
		var a author
		if err := rows.Scan(&a.groupID, &a.group, &a.id, &a.name); err != nil {
			return nil, err
		}
		authors = append(authors, a)
	}
	return authors, rows.Err()
}

type searchArgs struct {
	Query   string `json:"query" jsonschema:"topic or exact identifier to search for"`
	From    string `json:"from" jsonschema:"inclusive RFC3339 timestamp"`
	To      string `json:"to" jsonschema:"exclusive RFC3339 timestamp"`
	GroupID string `json:"group_id,omitempty" jsonschema:"optional group ID"`
	Limit   int    `json:"limit,omitempty" jsonschema:"total messages including context; default 15 and maximum 30"`
}

type message struct {
	ID         string    `json:"message_id"`
	GroupID    string    `json:"group_id"`
	GroupName  string    `json:"group_name"`
	AuthorName string    `json:"author_name"`
	SentAt     time.Time `json:"sent_at"`
	Text       string    `json:"text"`
	IsContext  bool      `json:"is_context"`
}

type searchResult struct {
	Messages []message `json:"messages"`
}

func searchParameters(a searchArgs) (time.Time, time.Time, int, error) {
	from, fromErr := time.Parse(time.RFC3339, a.From)
	to, toErr := time.Parse(time.RFC3339, a.To)
	if fromErr != nil || toErr != nil || !to.After(from) || to.Sub(from) > 31*24*time.Hour {
		return from, to, 0, fmt.Errorf("provide an RFC3339 time range greater than zero and up to 31 days")
	}
	if n := len([]rune(strings.TrimSpace(a.Query))); n == 0 || n > 300 {
		return from, to, 0, fmt.Errorf("query must contain 1–300 characters")
	}
	if a.Limit < 0 || a.Limit > 30 {
		return from, to, 0, fmt.Errorf("limit must be between 1 and 30 when set")
	}
	limit := a.Limit
	if limit == 0 {
		limit = 15
	}
	return from, to, limit, nil
}

func (s store) search(ctx context.Context, user, group, query string, from, to time.Time, limit int, vector []float32) (searchResult, error) {
	result := searchResult{Messages: []message{}}
	rows, err := s.db.Query(ctx, `
		WITH visible AS MATERIALIZED (
			SELECT m.* FROM chat_group_members member
			JOIN chat_messages m ON m.group_id = member.group_id
			WHERE member.user_id = $1 AND ($2 = '' OR m.group_id = $2)
			  AND m.sent_at >= $3 AND m.sent_at < $4
		), search_query AS (
			SELECT websearch_to_tsquery('russian', $5) AS lex
		), scored AS (
			SELECT v.id, v.group_id, v.sent_at,
				strpos(lower(v.body),lower($5)) > 0 AS exact_match,
				CASE WHEN to_tsvector('russian', v.body) @@ q.lex
                    THEN ts_rank(to_tsvector('russian', v.body), q.lex) ELSE 0 END AS lexical,
				(SELECT sum(a.x * b.x) FROM unnest(v.embedding) WITH ORDINALITY a(x,i)
				 JOIN unnest($6::real[]) WITH ORDINALITY b(x,i) USING (i)) AS semantic
			FROM visible v CROSS JOIN search_query q
		), matches AS (
			SELECT id, group_id, sent_at, row_number() OVER (
				ORDER BY exact_match DESC, (semantic + LEAST(lexical, 0.20)) DESC, id
			) AS rank
			FROM scored WHERE exact_match OR lexical > 0 OR semantic >= 0.80
			ORDER BY rank
			LIMIT $7
		), picked AS (
			SELECT id, rank, false AS is_context FROM matches
			UNION ALL
			SELECT neighbor.id, hits.rank, true
			FROM matches hits
			CROSS JOIN LATERAL (
				SELECT v.id FROM visible v
				WHERE v.group_id = hits.group_id AND v.id <> hits.id
				  AND v.sent_at BETWEEN hits.sent_at - interval '5 minutes' AND hits.sent_at + interval '5 minutes'
				ORDER BY abs(extract(epoch FROM (v.sent_at - hits.sent_at))), v.id
				LIMIT 2
			) neighbor
		), unique_messages AS (
			SELECT DISTINCT ON (id) id, rank, is_context FROM picked ORDER BY id, is_context, rank
		)
		SELECT v.id::text, v.group_id, g.name, a.display_name, v.sent_at, v.body, p.is_context
		FROM unique_messages p JOIN visible v USING (id)
		JOIN chat_groups g ON g.id = v.group_id
		JOIN chat_group_members a ON a.group_id = v.group_id AND a.user_id = v.author_id
		ORDER BY p.is_context, p.rank, v.sent_at, v.id
		LIMIT $7
	`, user, group, from, to, query, vector, limit)
	if err != nil {
		return result, err
	}
	return readMessages(rows)
}

func (s store) latest(ctx context.Context, user, group string) (searchResult, error) {
	rows, err := s.db.Query(ctx, `
		SELECT m.id::text, g.id, g.name, a.display_name, m.sent_at, m.body, false
		FROM chat_group_members member
		JOIN chat_groups g ON g.id = member.group_id
		JOIN chat_messages m ON m.group_id = g.id
		JOIN chat_group_members a ON a.group_id = g.id AND a.user_id = m.author_id
		WHERE member.user_id = $1 AND (
			$2 = '' OR g.id = $2 OR lower(g.name) = lower($2)
			OR lower(g.name || ' - ' || a.display_name) =
			   regexp_replace(lower($2), '[[:space:]]+[-–—][[:space:]]+', ' - ', 'g')
		)
		  AND m.sent_at <= now()
		ORDER BY m.sent_at DESC, m.id DESC LIMIT 10
	`, user, group)
	if err != nil {
		return searchResult{}, err
	}
	return readMessages(rows)
}

func readMessages(rows pgx.Rows) (searchResult, error) {
	result := searchResult{Messages: []message{}}
	defer rows.Close()
	for rows.Next() {
		var m message
		if err := rows.Scan(&m.ID, &m.GroupID, &m.GroupName, &m.AuthorName, &m.SentAt, &m.Text, &m.IsContext); err != nil {
			return result, err
		}
		m.SentAt = m.SentAt.UTC()
		result.Messages = append(result.Messages, m)
	}
	return result, rows.Err()
}
