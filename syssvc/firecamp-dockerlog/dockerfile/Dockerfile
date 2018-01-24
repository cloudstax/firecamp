FROM alpine:3.5

RUN apk update
RUN apk add --no-cache ca-certificates

RUN mkdir -p /run/docker/plugins /var/lib/firecamp

COPY firecamp-dockerlog firecamp-dockerlog

CMD ["firecamp-dockerlog"]

