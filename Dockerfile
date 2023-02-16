FROM golang:1.19.5-alpine

RUN addgroup -S butterbot && adduser -S butterbot -G butterbot
RUN apk add --no-cache git

WORKDIR /home/butterbot

ADD config.yaml .
ADD butterbot.go .
ADD go.mod .
ADD go.sum .

RUN go mod tidy
RUN go build -buildvcs=false -o butterbot
RUN chmod +x butterbot && chown butterbot:butterbot butterbot

USER butterbot

ENTRYPOINT ./butterbot