FROM alpine:3.5

RUN apk update
RUN apk add --no-cache ca-certificates

RUN mkdir -p /run/docker/plugins /mnt/firecamp

COPY firecamp-dockervolume firecamp-dockervolume

CMD ["firecamp-dockervolume"]

