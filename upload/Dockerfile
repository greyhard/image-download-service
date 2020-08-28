FROM golang:1.13-alpine

WORKDIR /go/src/app
COPY . .

RUN apk add git
RUN go get -d -v ./...
RUN go install -v ./...

CMD [ "app", "-dir=/upload", "-port=8080" ]