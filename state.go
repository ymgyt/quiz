package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	ttemplate "text/template"

	"go.uber.org/zap"
)

// State -
type State struct {
	Users    []*User             `json:"users"`
	Quiz     *Quiz               `json:"-"` // 答えも格納してるから直接は渡さない
	QuizIdx  int                 `json:"quiz_idx"`
	Config   *MatchConfig        `json:"-"`
	Contexts map[string]*Context `json:"-"`
	match    *Match              `json:"-"`
}

// StateView -
type StateView struct {
	*State
	ResultViews []*resultView
	UsersTable  string `json:"users_table"`
	QuizDiv     string `json:"quiz_div"`
}

type resultView struct {
	User  *User
	Items []resultItem
}

type resultItem struct {
	Result     *QuizResult
	AnswerOpen bool
}

var usersTableTmpl = template.Must(template.New("users").Funcs(template.FuncMap{
	"iterate":      tfIterate,
	"inc":          tfInc,
	"formatResult": tfFormatQuizResult,
}).Parse(`
<table>
	<thead>
		<tr>
			<th>User</th>
			{{- range $val := iterate .Config.QuizNum -}}
			<th> {{ inc $val }}</th>
			{{- end }}
		</tr>
	</thead>
	<tbody>
		{{- range .ResultViews }}
		<td><img class="avatar" src="{{ .User.AvatarURL }}" alt="{{ .User.Name }}"></td>
			{{range .Items }}
				{{ formatResult . }}
			{{ end }}
		{{ end }}
	</tbody>
</table>`))

/*
type QuizResult struct {
	NotYet    bool // まだ回答されていない
	QuizIdx   int
	OptionIdx int
	Correct   bool
}
*/

func tfFormatQuizResult(item resultItem) string {
	var class string
	if item.Result.NotYet {
		return "<td>?</td>"
	}
	if item.AnswerOpen {
		if item.Result.Correct {
			class = `class="correct"`
		} else {
			class = `class="wrong"`
		}
	}
	return fmt.Sprintf("<td %s>%d</td>", class, item.Result.OptionIdx)
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

func (v *StateView) encode() []byte {

	// userごとの回答状況
	var results []*resultView
	for _, u := range v.Users {
		ctx, found := v.State.Contexts[u.Name]
		if !found {
			// 最初はcontextがない
			results = append(resultView{
				User: u,
			})
		}
		rv := &resultView{User: u}
		for _, r := range ctx.Results {
			rv.Items = append(rv.Items, &resultItem{
				Result:     r,
				AnswerOpen: r.QuizIdx < v.match.currentQuiz,
			})
		}
		results = append(results, rv)
	}
	v.ResultViews = results

	var rb bytes.Buffer
	if err := usersTableTmpl.Execute(&rb, v); err != nil {
		v.State.match.logger.Error("template_execute", zap.Error(err))
		return []byte{}
	}
	v.UsersTable = rb.String()

	if v.Quiz != nil {
		var qb bytes.Buffer
		if err := quizDivTmpl.Execute(&qb, v); err != nil {
			v.State.match.logger.Error("template_execute", zap.Error(err))
			return []byte{}
		}
		v.QuizDiv = qb.String()
	}

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

func tfIterate(n int) []int {
	var ns = make([]int, n)
	for i := 0; i < n; i++ {
		ns[i] = i
	}
	return ns
}

func tfInc(n int) int {
	return n + 1
}
