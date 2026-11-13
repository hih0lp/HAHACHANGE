package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// formatCalendarBusy renders only intervals returned by the calendar MCP.
// This avoids adding an unverified slot when the model phrases a direct answer.
func formatCalendarBusy(result any) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	var response struct {
		Data struct {
			Calendars map[string]struct {
				Busy []struct {
					Start string `json:"start"`
					End   string `json:"end"`
				} `json:"busy"`
			} `json:"calendars"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	if len(response.Data.Calendars) == 0 {
		return "", errors.New("calendar busy data is missing")
	}
	zone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return "", err
	}
	var lines []string
	for _, calendar := range response.Data.Calendars {
		for _, slot := range calendar.Busy {
			start, startErr := time.Parse(time.RFC3339, slot.Start)
			end, endErr := time.Parse(time.RFC3339, slot.End)
			if startErr != nil || endErr != nil || !end.After(start) {
				return "", errors.New("invalid calendar busy interval")
			}
			start, end = start.In(zone), end.In(zone)
			if start.YearDay() == end.YearDay() && start.Year() == end.Year() {
				lines = append(lines, fmt.Sprintf("- %s %s–%s МСК", start.Format("02.01.2006"), start.Format("15:04"), end.Format("15:04")))
			} else {
				lines = append(lines, fmt.Sprintf("- %s – %s МСК", start.Format("02.01.2006 15:04"), end.Format("02.01.2006 15:04")))
			}
		}
	}
	if len(lines) == 0 {
		return "В проверенном календаре за указанный период нет занятых интервалов.", nil
	}
	return "По календарю наставник занят:\n" + strings.Join(lines, "\n"), nil
}
