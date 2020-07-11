FROM golang:1.11-alpine

RUN addgroup -S butterbot && adduser -S butterbot -G butterbot
RUN apk add --no-cache git
RUN go get gopkg.in/yaml.v2 github.com/davecgh/go-spew/spew
WORKDIR /home/butterbot
USER butterbot
ADD butterbot.go .
RUN go build -o butterbot
RUN chmod +x butterbot
ADD config.yaml .
ENTRYPOINT ./butterbot