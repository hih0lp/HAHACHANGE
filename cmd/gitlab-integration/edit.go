package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hackchange/avito-rag/internal/mockfixture"
)

func renameMockBranch(data []byte, r *http.Request, body []byte) ([]byte, error) {
	f, err := parseMockData(data)
	if err != nil {
		return nil, err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err = json.Unmarshal(body, &in); err != nil || !branchName.MatchString(in.Name) {
		return nil, errors.New("Укажите корректное новое имя ветки.")
	}
	id, err := strconv.Atoi(r.PathValue("project"))
	if err != nil {
		return nil, mockfixture.ErrItemNotFound
	}
	old := strings.TrimSuffix(r.PathValue("name"), "/")
	for i := range f.Projects {
		p := &f.Projects[i]
		if p.ID != id {
			continue
		}
		for j := range p.Branches {
			if p.Branches[j].Name != old {
				continue
			}
			if old == in.Name {
				return nil, errors.New("Новое имя совпадает с прежним.")
			}
			for _, other := range p.Branches {
				if other.Name == in.Name {
					return nil, errors.New("Ветка с таким именем уже существует.")
				}
			}
			p.Branches[j].Name = in.Name
			if p.DefaultBranch == old {
				p.DefaultBranch = in.Name
			}
			for k := range p.MRs {
				if p.MRs[k].Source == old {
					p.MRs[k].Source = in.Name
				}
				if p.MRs[k].Target == old {
					p.MRs[k].Target = in.Name
				}
			}
			return json.Marshal(f)
		}
		return nil, mockfixture.ErrItemNotFound
	}
	return nil, mockfixture.ErrItemNotFound
}
