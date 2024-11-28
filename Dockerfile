FROM golang:1.23 AS builder

COPY . /go/src/app
WORKDIR /go/src/app

ENV GO111MODULE=on

RUN CGO_ENABLED=0 GOOS=linux go build -o app

RUN git log -1 --oneline > version.txt

FROM builder AS test 
WORKDIR /go/src/app
COPY tests/run_tests.sh run_tests.sh
ENTRYPOINT [ "sh", "./run_tests.sh" ]

FROM alpine:latest 
WORKDIR /root/
COPY --from=builder /go/src/app/app .
COPY --from=builder /go/src/app/pkg/resources ./pkg/resources
COPY --from=builder /go/src/app/config.json .
COPY --from=builder /go/src/app/version.txt .

EXPOSE 8080

ENTRYPOINT ["./app"]
