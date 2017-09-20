FROM golang:1.8-alpine

RUN mkdir -p /go/src/github.com/cloudstax

RUN set -ex \
    && apk add --no-cache --virtual .build-deps \
    gcc libc-dev

COPY build-entrypoint.sh /
ENTRYPOINT ["/build-entrypoint.sh"]
