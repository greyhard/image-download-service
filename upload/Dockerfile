############################
# STEP 1 build executable binary
############################
FROM golang:1.17-alpine as builder

WORKDIR /go/src/app
COPY . .

RUN go get -d -v ./...
RUN go install -v ./...
RUN go build -o /go/bin/imageservice

############################
# STEP 2 build a small image
############################
FROM  golang:1.17-alpine
WORKDIR /go/src/app
COPY --from=builder /go/bin/imageservice /go/bin/imageservice

CMD [ "/go/bin/imageservice", "-dir=/upload", "-port=8080" ]