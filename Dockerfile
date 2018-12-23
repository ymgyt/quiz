FROM golang:1.11.2-alpine3.8

WORKDIR /go/src/github.com/ymgyt/quiz

# RUN apk --no-cache add ca-certificates

COPY . .

RUN CGO_ENABLED=0 go build -o quiz-bin

EXPOSE 443
EXPOSE 9002

ENTRYPOINT [ "/go/src/github.com/ymgyt/quiz/quiz-bin" ]
