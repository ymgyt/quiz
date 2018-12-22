package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	ttemplate "text/template"

	"github.com/davecgh/go-spew/spew"
	"go.uber.org/zap"
)

// State -
type State struct {
	Users    []*User
	Quiz     *Quiz
	QuizIdx  int
	Config   *MatchConfig
	Contexts map[string]*Context
	match    *Match
}

// StateView -
type StateView struct {
	*State    `json:"-"`
	UsersView string `json:"users_view"`
	QuizIdx   int    `json:"quiz_idx"`
	QuizView  string `json:"quiz_view"`
}

func (v *StateView) users() string {
	var b bytes.Buffer
	b.WriteString(`
<table>
	<thead>
		<tr>
			<th>User</th>`)
	n := v.State.Config.QuizNum
	for i := 1; i <= n; i++ {
		b.WriteString(fmt.Sprintf("<th>%d</th>", i))
	}
	b.WriteString(`</tr></thead><tbody>`)

	for _, user := range v.Users {
		ctx, found := v.Contexts[user.Name]
		if !found {
			continue
		}
		spew.Dump(ctx)
		b.WriteString(fmt.Sprintf(`<tr><td><img class="avatar" src="%s" alt="%s"></td>`, user.AvatarURL, user.Name))
		for _, qr := range ctx.Results {
			if !qr.OptionSubmitted {
				b.WriteString(`<td>?</td>`)
				continue
			}
			// optionidx 0 => 選択肢1なので
			optionNum := qr.OptionIdx + 1
			if *qr.UserCanGetTheirResult {
				class := "wrong"
				if qr.Correct {
					class = "correct"
				}
				b.WriteString(fmt.Sprintf(`<td class="%s">%d</td>`, class, optionNum))
			} else {
				b.WriteString(fmt.Sprintf("<td>%d</td>", optionNum))
			}
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</tbody></table>`)

	return b.String()
}

// htmlそのまま書くので、escapeしないために、text templateを利用する
var quizDivTmpl = ttemplate.Must(ttemplate.New("quiz").Funcs(ttemplate.FuncMap{}).Parse(`
<div class="quiz-data">
	<div class="quiz-creater">
		<span>出題者</span>
		<img class="avatar" src="{{ .Quiz.User.AvatarURL }}" alt="{{ .Quiz.User.Name}}">
	</div>
	<div class="quiz-content">
		{{ .Quiz.DescriptionHTML }}
	</div>
	<div class="quiz-options">
	{{ range .Quiz.Options }}
	  <div class="quiz-option">
		  <input type="radio" name="option-index-radio" value="{{ .Index }}">
		  <div class="quiz-option-description">{{ .Description }}</div>
	  </div>
	{{ end }}
	</div>
	<button type="button" id="quiz-submit-btn">Submit</button>
</div>
`))

func (v *StateView) quiz() string {
	if v.Quiz == nil {
		return ""
	}
	var b bytes.Buffer
	if err := quizDivTmpl.Execute(&b, v); err != nil {
		v.State.match.logger.Error("template_execute", zap.Error(err))
		return ""
	}
	return b.String()
}

func (v *StateView) encode() []byte {

	v.QuizView = v.quiz()
	v.UsersView = v.users()
	v.QuizIdx = v.State.QuizIdx

	encoded, err := json.Marshal(v)
	if err != nil {
		v.State.match.logger.Error("mashal fail", zap.Error(err))
		return []byte{}
	}
	return encoded
}

func (s *State) encode() []byte {
	return (&StateView{State: s}).encode()
}
