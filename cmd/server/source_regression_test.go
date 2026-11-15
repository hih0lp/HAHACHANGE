package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDocumentAnswersVerifyCitationOrReturnSource(t *testing.T) {
	for _, tc := range []struct {
		name, generated, want string
		tries                 int
	}{
		{"verified", "Ревью проходит [D1]. [левый сайт](https://example.invalid)", "Ревью проходит [playbook/rules.md](https://github.com/avito-tech/playbook/blob/master/rules.md)", 1},
		{"unverified", "Утверждение без источника [D9]", "Вот материалы, которые вернул поиск", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/embed":
					fmt.Fprint(w, "[[1,0]]")
				case "/collections/playbook/points/query":
					fmt.Fprint(w, `{"result":{"points":[{"score":0.9,"payload":{"path":"playbook/rules.md","title":"Правила","text":"Команда проводит ревью."}}]}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer upstream.Close()
			calls := 0
			llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input struct{ Messages []struct{ Content string } }
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				answer := tc.generated
				if strings.Contains(input.Messages[0].Content, "Classify the current question") {
					answer = `{"needs_document_evidence":true,"document_query":"правила ревью"}`
				} else {
					calls++
					if calls == 2 && !strings.Contains(input.Messages[0].Content, "Ответ без ссылок") {
						t.Error("missing citation retry")
					}
				}
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, answer)
			}))
			defer llm.Close()
			a := answerer{service: rag.New(rag.Config{EmbeddingsURL: upstream.URL, QdrantURL: upstream.URL, CollectionName: "playbook"}), url: llm.URL}
			got, _, err := a.Ask(distributed.WithTeamToken(context.Background(), "intern"), "Где описано ревью?")
			if err != nil || calls != tc.tries || !strings.Contains(got, tc.want) || strings.Contains(got, "https://example.invalid") || strings.Contains(got, "[D9]") {
				t.Fatalf("answer=%q requests=%d error=%v", got, calls, err)
			}
		})
	}
}

func TestAssistantDoesNotPresentUnverifiedServiceData(t *testing.T) {
	tests := []struct {
		name, plan, mode, want string
		team, jira, calendar   bool
	}{
		{"no Jira connection", `{"needs_my_jira_issues":true,"needs_jira_issue":true}`, "off", "Jira сейчас не подключена", false, false, false},
		{"missing Jira key", `{"needs_jira_issue":true}`, "mock", "Не удалось определить ключ", false, true, false},
		{"task missing from service", `{"needs_jira_issue":true,"jira_issue_keys":["SCRUM-2"]}`, "mock", "Не удалось проверить карточку Jira SCRUM-2", false, true, false},
		{"broken Jira response", `{"needs_jira_issue":true,"jira_issue_keys":["SCRUM-3"]}`, "mock", "Не удалось прочитать карточку Jira SCRUM-3", false, true, false},
		{"team not available", `{"needs_team_directory":true}`, "off", "Сервис команды сейчас не подключён", false, false, false},
		{"team profile has no mapping", `{"needs_my_jira_issues":true}`, "mock", "Jira-аккаунт пока не сопоставлен", true, true, false},
		{"calendar returns invalid events", `{"needs_events":true,"needs_availability":true,"needs_company_events":true,"period_kind":"current_month"}`, "off", "Не удалось прочитать события календаря наставника", false, false, true},
		{"outside current month", `{"needs_events":true,"period_kind":"explicit","time_min":"2020-01-01T00:00:00+03:00","time_max":"2020-01-02T00:00:00+03:00"}`, "off", "Не удалось определить период календаря", false, false, true},
		{"no catalogue", `{"needs_access_check":true}`, "off", "Не удалось проверить каталог ресурсов", false, false, false},
		{"forms offline", `{"needs_form":true}`, "off", "Список форм сейчас недоступен", false, false, false},
		{"group not resolved", `{"latest_group_messages":true}`, "off", "Сейчас не удалось подключиться", false, false, false},
		{"model asks for expired information", `{"needs_recommendation":true}`, "off", "не удалось проверить данные команды, задачи Jira", false, false, false},
		{"no matching documentation", `{"needs_document_evidence":true}`, "off", "Не удалось найти фрагменты", false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, tc.plan)
			}))
			defer llm.Close()
			stub := mcp.NewServer(&mcp.Implementation{Name: "source", Version: "1"}, nil)
			if tc.jira {
				mcp.AddTool(stub, &mcp.Tool{Name: "jira_get_issue"}, func(_ context.Context, _ *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, map[string]any, error) {
					if args["key"] == "SCRUM-2" {
						return nil, nil, errors.New("issue missing")
					}
					return nil, map[string]any{"key": 12}, nil
				})
			}
			if tc.team {
				mcp.AddTool(stub, &mcp.Tool{Name: "team_profile"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]any, error) {
					return nil, map[string]any{"name": "Инженер"}, nil
				})
			}
			if tc.calendar {
				mcp.AddTool(stub, &mcp.Tool{Name: "calendar_onboarding_events"}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, map[string]any, error) {
					return nil, map[string]any{"data": "bad schema"}, nil
				})
			}
			mock := testMCPServer(t, stub)
			defer mock.Close()
			a := answerer{service: rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1"}), url: llm.URL, jiraMode: tc.mode}
			if tc.team {
				a.teamMCPURL = mock.URL
			}
			if tc.jira {
				a.mcpURL = mock.URL
			}
			if tc.calendar {
				a.calendarMCPURL = mock.URL
			}
			got, _, err := a.Ask(distributed.WithTeamToken(context.Background(), "intern"), "Нужна проверка рабочего вопроса")
			if err != nil || !strings.Contains(got, tc.want) {
				t.Fatalf("answer=%q error=%v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestGitLabAnswerRequiresActualToolResult(t *testing.T) {
	for _, tc := range []struct {
		mode, content, want string
		calls               int
	}{
		{"text tool call", `{"tool_call":{"name":"gitlab_branches","arguments":{"project":"demo/first"}}}`, "Ветка main проверена", 1},
		{"invented tool", `{"tool_call":{"name":"unregistered_tool","arguments":{}}}`, "Не удалось подтвердить запрошенные данные в GitLab", 0},
		{"promise only", "Я вызову инструмент.", "Не удалось подтвердить запрошенные данные в GitLab", 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			calls := 0
			fixture := mcp.NewServer(&mcp.Implementation{Name: "source", Version: "1"}, nil)
			mcp.AddTool(fixture, &mcp.Tool{Name: "gitlab_branches"}, func(_ context.Context, _ *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, map[string]any, error) {
				calls++
				if in["project"] != "demo/first" {
					t.Error(in)
				}
				return nil, map[string]any{"branch": "main"}, nil
			})
			gitlab := testMCPServer(t, fixture)
			defer gitlab.Close()
			completions := 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct{ Messages []struct{ Content string } }
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				response := `{"needs_gitlab":true}`
				if !strings.Contains(payload.Messages[0].Content, "Classify the current question") {
					completions++
					response = tc.content
					if completions > 1 {
						response = "Ветка main проверена"
						if tc.calls == 1 && !strings.Contains(payload.Messages[len(payload.Messages)-1].Content, `"branch":"main"`) {
							t.Error("tool data absent from generated answer context")
						}
					}
				}
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, response)
			}))
			defer model.Close()
			a := answerer{service: rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1"}), url: model.URL, gitLabMCPURL: gitlab.URL}
			answer, _, err := a.Ask(distributed.WithTeamToken(context.Background(), "intern"), "Проверь конкретную ветку GitLab")
			if err != nil || calls != tc.calls || !strings.Contains(answer, tc.want) {
				t.Fatalf("answer=%q tool calls=%d err=%v", answer, calls, err)
			}
		})
	}
}

func TestAssistantRecallsOlderTurnUsingTopicRatherThanReplayingHistory(t *testing.T) {
	prior := make([]chatContextTurn, recentChatLimit)
	for i := range prior {
		prior[i] = chatContextTurn{Question: "Современный вопрос", Answer: "Недавний ответ"}
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Messages []struct{ Content string } }
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		content := `{"memory_query":"  название проекта "}`
		if !strings.Contains(payload.Messages[0].Content, "memory_query") {
			if !strings.Contains(payload.Messages[1].Content, "Янтарь") {
				t.Error("missing older evidence")
			}
			content = "Мы назвали проект Янтарь."
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, content)
	}))
	defer model.Close()
	a := answerer{service: rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1"}), url: model.URL}
	lookup := func(_ context.Context, topic string) ([]oldChatTurn, error) {
		if topic != "название проекта" {
			t.Error(topic)
		}
		return []oldChatTurn{{Question: "Название проекта", Answer: "Янтарь"}}, nil
	}
	answer, _, err := a.answerWithContext(context.Background(), "Как назвали проект в начале чата?", prior, lookup)
	if err != nil || !strings.Contains(answer, "Янтарь") {
		t.Fatalf("%q %v", answer, err)
	}
}

func TestPlannerKeepsLMStudioAndOllamaJSONRequestsCompatible(t *testing.T) {
	for _, tc := range []struct {
		noThink, wantFormat bool
	}{
		{false, true}, {true, false},
	} {
		t.Run(fmt.Sprint(tc.noThink), func(t *testing.T) {
			t.Setenv("LLM_PLANNER_NO_THINK", fmt.Sprint(tc.noThink))
			t.Setenv("LLM_REASONING_EFFORT", "none")
			t.Setenv("LLM_API_KEY", "test-only-key")
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ResponseFormat map[string]any `json:"response_format"`
					MaxTokens      int            `json:"max_tokens"`
					Messages       []struct{ Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if (request.ResponseFormat != nil) != tc.wantFormat || strings.HasSuffix(request.Messages[1].Content, "/no_think") != tc.noThink ||
					request.MaxTokens != 512 || r.Header.Get("Authorization") != "Bearer test-only-key" {
					t.Errorf("unexpected planner options: %+v", request)
				}
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, "```json\n{\"needs_my_jira_issues\":true}\n```")
			}))
			defer model.Close()
			got, err := (answerer{url: model.URL}).requestPlan(context.Background(), "fixture", map[string]any{"question": "Мои задачи"})
			if err != nil || !got.NeedsMyJiraIssues {
				t.Fatalf("plan=%+v err=%v", got, err)
			}
		})
	}
}

func TestAssistantReadsGroupMembershipFromService(t *testing.T) {
	fixture := mcp.NewServer(&mcp.Implementation{Name: "messenger", Version: "1"}, nil)
	mcp.AddTool(fixture, &mcp.Tool{Name: "list_my_groups"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"groups": []map[string]string{{"name": "Моя группа"}}}, nil
	})
	messenger := testMCPServer(t, fixture)
	defer messenger.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, `{"needs_group_memberships":true}`)
	}))
	defer model.Close()
	a := answerer{service: rag.New(rag.Config{EmbeddingsURL: "http://127.0.0.1:1"}), url: model.URL, groupChatMCPURL: messenger.URL}
	got, _, err := a.Ask(distributed.WithTeamToken(context.Background(), "intern"), "В каких группах я состою?")
	if err != nil || !strings.Contains(got, "Моя группа") {
		t.Fatalf("group answer=%q err=%v", got, err)
	}
}

func TestReadOnlyGitLabOperationsHandlePartialProviderData(t *testing.T) {
	tests := []struct {
		name, tool string
		data       any
		plan       calendarPlan
		want       string
	}{
		{"empty project list", "gitlab_projects", []any{}, calendarPlan{GitLabOperation: "commits"}, "не вернул проектов"},
		{"multiple projects", "gitlab_projects", []gitlabProject{{ID: 2, Path: "demo/a"}, {ID: 3, Path: "demo/b"}}, calendarPlan{GitLabOperation: "commits"}, "demo/a, demo/b"},
		{"empty branch", "gitlab_commits", []any{}, calendarPlan{GitLabOperation: "commits", GitLabRef: "release"}, "не вернул коммитов"},
		{"ambiguous diff", "gitlab_commits", []gitlabCommit{{ID: "aaa"}, {ID: "bbb"}}, calendarPlan{GitLabOperation: "commit_diff"}, "Укажите SHA"},
		{"renamed file", "gitlab_commit_diff", []map[string]string{{"old_path": "old", "new_path": "new", "diff": "-before\n+after"}}, calendarPlan{GitLabOperation: "commit_diff", GitLabCommit: "aaa"}, "old → new"},
		{"empty diff", "gitlab_commit_diff", []any{}, calendarPlan{GitLabOperation: "commit_diff", GitLabCommit: "aaa"}, "не вернул изменений"},
		{"list MRs", "gitlab_merge_requests", []gitlabMR{{IID: 12, Title: "Change"}}, calendarPlan{GitLabOperation: "merge_requests"}, "!12 — Change"},
		{"approval ambiguity", "gitlab_merge_requests", []gitlabMR{{IID: 12}, {IID: 13}}, calendarPlan{GitLabOperation: "approval"}, "Укажите номер"},
		{"approval confirmed", "gitlab_approval_state", map[string]any{"rules": []map[string]any{{"name": "Owner", "approved": true, "approvals_required": 1}}}, calendarPlan{GitLabOperation: "approval", GitLabMR: "!12"}, "Owner: одобрено"},
		{"approval missing", "gitlab_approval_state", map[string]any{"rules": []map[string]any{{"name": "Owner"}}}, calendarPlan{GitLabOperation: "approval", GitLabMR: "!12"}, "не вернул подтверждённый статус"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "gitlab-fixture", Version: "1"}, nil)
			mcp.AddTool(server, &mcp.Tool{Name: "gitlab_projects"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]any, error) {
				if tc.tool == "gitlab_projects" {
					return nil, map[string]any{"data": tc.data}, nil
				}
				return nil, map[string]any{"data": []gitlabProject{{ID: 2, Path: "demo/project"}}}, nil
			})
			if tc.tool != "gitlab_projects" {
				mcp.AddTool(server, &mcp.Tool{Name: tc.tool}, func(_ context.Context, _ *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, map[string]any, error) {
					if args["project"] != "2" || tc.plan.GitLabRef != "" && args["ref"] != tc.plan.GitLabRef {
						t.Errorf("tool args=%v", args)
					}
					return nil, map[string]any{"data": tc.data}, nil
				})
			}
			mock := testMCPServer(t, server)
			defer mock.Close()
			got := (answerer{gitLabMCPURL: mock.URL}).gitLabReadAnswer(context.Background(), "intern", tc.plan)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("expected %q in %q", tc.want, got)
			}
		})
	}
}
