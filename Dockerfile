FROM golang:1.22.0-bullseye

RUN useradd -ms /bin/bash butterbot
RUN apt install -y git

WORKDIR /home/butterbot

ADD butterbot.go .
ADD go.mod .
ADD go.sum .

RUN go mod tidy
RUN go build -buildvcs=false -o butterbot
RUN chmod +x butterbot && chown butterbot:butterbot butterbot

USER butterbot

ENTRYPOINT ./butterbot