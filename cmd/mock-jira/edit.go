package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hackchange/avito-rag/internal/mockfixture"
)

func updateIssue(data []byte, r *http.Request, body []byte) ([]byte, error) {
	f, err := parseFixture(data)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(body, &fields); err != nil || len(fields) == 0 {
		return nil, errors.New("Укажите изменяемые поля задачи.")
	}
	for name := range fields {
		switch name {
		case "summary", "description", "due_date", "due_at":
		default:
			return nil, errors.New("Неизвестное поле задачи: " + name)
		}
	}
	for i := range f.Issues {
		if f.Issues[i].Key != r.PathValue("key") {
			continue
		}
		v := &f.Issues[i]
		for name, raw := range fields {
			var value string
			if string(raw) != "null" {
				if err := json.Unmarshal(raw, &value); err != nil {
					return nil, errors.New("Поле " + name + " должно быть строкой или null.")
				}
			}
			switch name {
			case "summary":
				v.Summary = value
			case "description":
				v.Description = value
			case "due_date":
				v.DueDate = value
			case "due_at":
				v.DueAt = value
			}
		}
		return json.Marshal(f)
	}
	return nil, mockfixture.ErrItemNotFound
}
