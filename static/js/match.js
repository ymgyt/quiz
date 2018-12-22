let currentMsg

class Match {
    constructor(conn) {
        this.dom = {}
        this.dom.status = document.getElementById('status')
        this.dom.quiz = document.getElementById('quiz')

        this.id_token = query('id_token')
        // userの回答状況
        this.userStatus = "initial"
        this.quizIdx = -1

        this.onmessage = this.onmessage.bind(this)
        this.onclose = this.onclose.bind(this)
        this.onsubmit = this.onsubmit.bind(this)

        conn.onmessage = this.onmessage
        conn.onclose = this.onclose
    }

    updateUserState(usersTable) {
        this.dom.status.innerHTML = ''
        this.dom.status.innerHTML = usersTable
    }
    updateQuiz(quiz) {
        // 制御必要
        this.dom.quiz.innerHTML = ''
        this.dom.quiz.innerHTML = quiz
    }
    // submit buttonも毎回serverからinjectされるので、eventの設定が必要
    handleSubmit() {
        const submitBtn = document.getElementById('quiz-submit-btn')
        if (submitBtn) {
            submitBtn.addEventListener('click', this.onsubmit)
        }
    }

    updateState(state) {
        this.updateUserState(state.users_view)
        this.updateQuiz(state.quiz_view)
        this.quizIdx = state.quiz_idx
        this.handleSubmit()
    }

    onmessage(event) {
        const state = JSON.parse(event.data)
        console.log("msg", state)
        currentMsg = state
        this.updateState(state)
    }
    onclose(event) {
        console.log("close", event)
    }
    onsubmit(event) {
        let optIdx = -1
        const options = document.querySelectorAll('input[name="option-index-radio"]')
        for (const opt of options) {
            if (opt.checked) { optIdx = opt.value }
        }
        console.log("idx", this.quizIdx, "answer", optIdx)
        const ep = `/api/v1/${window.location.pathname}/submission`
        fetch(ep, {
            method: 'POST',
            headers: {
                'Authorization': this.id_token,
            },
            body: JSON.stringify({
                quiz_idx: this.quizIdx,
                option_idx: Number(optIdx),
            })
        })
    }
}

let gConn
let gMatch

const init = () => {
    const conn = new WebSocket(wsEndpoint())
    gConn = conn

    const match = new Match(conn)
    gMatch = match


    if (!window["WebSocket"]) {
        console.log("your browser does not support websocket.")
    }
}

const wsEndpoint = () => {
    const ws = `ws://${window.location.hostname}:9002${window.location.pathname}${window.location.search}`
    return ws
}

const query = key => {
    let found = ""
    window.location.search.substr(1).split('&').map(kv => kv.split('=')).forEach(kv => {if (kv[0] === key) { found = kv[1] }})
    return found
}

window.addEventListener('load', init)