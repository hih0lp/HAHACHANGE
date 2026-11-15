package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hackchange/avito-rag/internal/mockfixture"
)

func updateEvent(data []byte, r *http.Request, body []byte) ([]byte, error) {
	f, err := parseFixture(data)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(body, &fields); err != nil || len(fields) == 0 {
		return nil, errors.New("Укажите дату начала или окончания.")
	}
	for name := range fields {
		if name != "start" && name != "end" {
			return nil, errors.New("Неизвестное поле события: " + name)
		}
	}
	for i := range f.Events {
		if f.Events[i].ID != r.PathValue("id") {
			continue
		}
		v := &f.Events[i]
		for name, raw := range fields {
			var date struct {
				DateTime string `json:"dateTime"`
				TimeZone string `json:"timeZone"`
			}
			if len(raw) > 0 && raw[0] == '"' {
				if err := json.Unmarshal(raw, &date.DateTime); err != nil {
					return nil, err
				}
				date.TimeZone = f.TimeZone
			} else if err := json.Unmarshal(raw, &date); err != nil {
				return nil, err
			}
			if date.DateTime == "" {
				return nil, errors.New("dateTime обязателен.")
			}
			if date.TimeZone == "" {
				date.TimeZone = f.TimeZone
			}
			if name == "start" {
				v.Start.DateTime, v.Start.TimeZone = date.DateTime, date.TimeZone
			} else {
				v.End.DateTime, v.End.TimeZone = date.DateTime, date.TimeZone
			}
		}
		return json.Marshal(f)
	}
	return nil, mockfixture.ErrItemNotFound
}
