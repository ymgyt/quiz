class QuizForm {
    constructor(cfg = {}){
        this.cfg = cfg
        this.dom = {}
        this.dom.quizDescription = document.getElementById('quiz-description')
        this.dom.options = [
         document.getElementById('option-one-description'),
         document.getElementById('option-two-description'),
         document.getElementById('option-three-description'),
         document.getElementById('option-four-description')
        ]
        // たぶんもっときれいにやれる
        this.dom.optionRadios = [
         document.getElementById('option-one-radio'),
         document.getElementById('option-two-radio'),
         document.getElementById('option-three-radio'),
         document.getElementById('option-four-radio')
        ]
        this.dom.answerDescription = document.getElementById('answer-description')

        this.id_token = query('id_token')
        this.save = this.save.bind(this)

        // add event
        document.getElementById('save-btn').addEventListener('click', this.save, false)
    }

    quiz() {
        const q =  {
            "description_md": this.dom.quizDescription.value,
            "options": [
                { index: 0, description: this.dom.options[0].value, is_answer: false},
                { index: 1, description: this.dom.options[1].value, is_answer: false},
                { index: 2, description: this.dom.options[2].value, is_answer: false},
                { index: 3, description: this.dom.options[3].value, is_answer: false},
            ],
            "answer_description": this.dom.answerDescription.value
        }
        const answerIdx = this.answerOptionIndex()
        q.options.forEach((opt, idx) => {
            if (opt.index == answerIdx) { opt.is_answer = true }
        })

        return q
    }

    answerOptionIndex() {
        const options = document.getElementsByClassName('option-radio')
        for (const opt of options) {
            if (opt.checked) { return opt.value }
        }
        return 0
    }

    save(){
        const quiz = this.quiz()
        console.log("save quiz", quiz)
        fetch(this.cfg.endpoints.save_quiz, {
            method: "POST",
            headers: this.headers(),
            body: JSON.stringify(quiz),
        })
        .then(res => {
            if (res.ok) {
                res.json().then(res => {
                    const href = this.cfg.endpoints.render_quiz + res.data.id +"?id_token=" + this.id_token
                    window.location.href = href
                })
            }
        })
    }

    populate() {
        fetch(this.cfg.endpoints.api_prefix + window.location.pathname,{
            method: "GET",
            headers: this.headers(),
        })
        .then(res => {
            if (res.ok) {
                res.json().then(res => {
                    console.log("fetch quiz", res)
                    this.bindQuiz(res.data)
                })
            }
        })
    }

    bindQuiz(q) {
        this.dom.quizDescription.textContent = q.description_md
        for (const opt of q.options) {
            const input = this.dom.options[opt.index]
            const radio = this.dom.optionRadios[opt.index]
            input.value = opt.description
            radio.checked = opt.is_answer
        }
        this.dom.answerDescription.value  = q.answer_description
    }

    headers() {
        return { "Authorization": this.id_token }
    }
}

const query = key => {
    let found = ""
    window.location.search.substr(1).split('&').map(kv => kv.split('=')).forEach(kv => {if (kv[0] === key) { found = kv[1] }})
    return found
}

let gQuiz

const init = () => {
    const cfg = {
        "endpoints": {
            "save_quiz": "/api/v1/quiz/new",
            "render_quiz": "/quiz/",
            "api_prefix": "/api/v1/",
        },
    }

    const quiz = new QuizForm(cfg)

    // 必要ない?
    // window.history.pushState(null, '', window.location.pathname)

    if (!window.location.pathname.endsWith('new')) {
        quiz.populate()
    }

    // for debug
    gQuiz = quiz
}

window.addEventListener('load', init)