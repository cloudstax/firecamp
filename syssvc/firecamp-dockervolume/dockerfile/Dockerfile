FROM alpine:3.5

RUN apk update
RUN apk add --no-cache xfsprogs e2fsprogs ca-certificates

RUN mkdir -p /run/docker/plugins /var/lib/firecamp /mnt/firecamp

COPY logrecycle.sh logrecycle.sh
COPY docker-entrypoint.sh docker-entrypoint.sh
COPY firecamp-dockervolume firecamp-dockervolume

CMD ["firecamp-dockervolume"]

