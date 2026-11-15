package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGroupChatAnswersUseAuthorizedMCPResults(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "chat-test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "list_my_groups"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"groups": []map[string]string{{"name": "Команда продукта"}}}, nil
	})
	messages := map[string]any{"messages": []map[string]any{{"group_name": "Команда продукта", "author_name": "Наставник", "sent_at": "2026-11-15T12:00:00+03:00", "text": "Обсуждаем запуск"}}}
	mcp.AddTool(server, &mcp.Tool{Name: "latest_group_messages"}, func(context.Context, *mcp.CallToolRequest, struct {
		Group string `json:"group"`
	}) (*mcp.CallToolResult, any, error) { return nil, messages, nil })
	mcp.AddTool(server, &mcp.Tool{Name: "search_group_messages"}, func(context.Context, *mcp.CallToolRequest, struct {
		Query   string `json:"query"`
		GroupID string `json:"group_id"`
		From    string `json:"from"`
		To      string `json:"to"`
		Limit   int    `json:"limit"`
	}) (*mcp.CallToolResult, any, error) {
		return nil, messages, nil
	})
	httpServer := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer httpServer.Close()
	a := answerer{groupChatMCPURL: httpServer.URL}
	ctx := context.Background()
	if got := a.groupMembershipsAnswer(ctx, "signed"); !strings.Contains(got, "Команда продукта") {
		t.Fatal(got)
	}
	loc, _ := time.LoadLocation("Europe/Moscow")
	if got := a.latestGroupMessages(ctx, "signed", "product", loc); !strings.Contains(got, "15.11 12:00 МСК") || !strings.Contains(got, "Обсуждаем запуск") {
		t.Fatal(got)
	}
	now := time.Date(2026, 11, 15, 15, 0, 0, 0, loc)
	plan := calendarPlan{ChatGroup: "product", ChatQuery: "запуск"}
	if got := a.groupTopicAnswer(ctx, "signed", plan, now); !strings.Contains(got, "Обсуждаем запуск") {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		plan calendarPlan
		want string
	}{
		{calendarPlan{}, "Укажите название группы"},
		{calendarPlan{ChatGroup: "product"}, "Не удалось определить тему"},
		{calendarPlan{ChatGroup: "product", ChatQuery: "запуск", ChatFrom: "bad", ChatTo: "2026-11-15T15:00:00+03:00"}, "Укажите период"},
	} {
		if got := a.groupTopicAnswer(ctx, "signed", tc.plan, now); !strings.Contains(got, tc.want) {
			t.Fatalf("%+v: %s", tc.plan, got)
		}
	}
	if got := formatMessengerMessages(map[string]any{"messages": []any{}}, loc, "Heading", "unavailable", "empty"); got != "empty" {
		t.Fatal(got)
	}
	if got := formatMessengerMessages(map[string]any{"wrong": true}, loc, "Heading", "unavailable", "empty"); got != "unavailable" {
		t.Fatal(got)
	}
}
