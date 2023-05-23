FROM golang:1.19.9-alpine3.18 as builder
WORKDIR /go/src
ADD . /go/src/
ENV GOPROXY https://goproxy.cn
RUN go build -o /go/bin/samplecontroller .

FROM alpine:3.18
LABEL authors="codexiaxie"
WORKDIR /home
COPY --from=builder /go/bin/samplecontroller .

ENTRYPOINT ["./samplecontroller"]