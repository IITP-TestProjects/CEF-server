ARG GO_VER=1.24
ARG ALPINE_VER=3.20
ARG PORT

FROM alpine:${ALPINE_VER} AS peer-base
RUN apk add --no-cache tzdata

RUN echo 'hosts: files dns' > /etc/nsswitch.conf

FROM golang:${GO_VER}-alpine${ALPINE_VER} AS golang
RUN apk add --no-cache \
	bash \
	gcc \
	git \
	make \
	musl-dev
ADD . $GOPATH/src/test_sub
WORKDIR $GOPATH/src/test_sub

FROM golang AS peer
RUN go build -o server

FROM peer-base
COPY --from=peer /go/src/test_sub /usr/local/bin
EXPOSE 50051
CMD ["server"]