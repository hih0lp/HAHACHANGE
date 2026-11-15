package main

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var accessFormDestinationQuestion = regexp.MustCompile("^(?:(?:куда|где|как) (?:мне )?(?:отправить|подать|направить|оформить|найти) (?:запрос|заявку|форму) на доступ|где (?:форма|ссылка на форму) (?:заявки )?на доступ)$")

func isAccessFormDestinationQuestion(question string) bool {
	question = strings.ToLower(strings.Join(strings.Fields(question), " "))
	question = strings.TrimRight(question, " ?!.")
	return accessFormDestinationQuestion.MatchString(question)
}

func (a answerer) listForms(ctx context.Context, token string) ([]formChoice, error) {
	if a.rightsMCPURL == "" {
		return nil, errors.New("rights catalogue endpoint is not configured")
	}
	result, err := mcpTeamTool(ctx, a.rightsMCPURL, "", token, "rights_forms_list", map[string]any{})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var catalogue struct {
		Forms []formChoice `json:"forms"`
	}
	if err := json.Unmarshal(data, &catalogue); err != nil {
		return nil, err
	}
	return catalogue.Forms, nil
}

func (a answerer) accessFormDestination(ctx context.Context, token string) string {
	forms, err := a.listForms(ctx, token)
	if err != nil {
		return "Сейчас сервис доступов не отвечает. Попробуйте запросить форму чуть позже."
	}
	for _, form := range forms {
		if form.Kind == "access" && form.ID != "" && form.URL == "/forms/"+form.ID {
			return "Подайте запрос через форму «Заявка на доступ» прямо в Бобби. Выберите ресурс и нужное право, затем проверьте и подтвердите отправку.\n\n[Открыть заявку на доступ](" + form.URL + ")"
		}
	}
	return "В каталоге сервиса доступов сейчас нет доступной вам формы заявки."
}

func isQuestionEcho(question, answer string) bool {
	normalize := func(text string) string {
		text = strings.ToLower(strings.Join(strings.Fields(text), " "))
		return strings.Trim(text, " ?!.,:;«»„“”\"'_*`")
	}
	want := normalize(question)
	return want != "" && normalize(answer) == want
}
