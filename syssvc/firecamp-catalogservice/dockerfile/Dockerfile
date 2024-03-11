#FROM debian:jessie-slim
FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y ca-certificates

RUN mkdir -p /var/log/firecamp

COPY firecamp-catalogservice /
COPY docker-entrypoint.sh /

ENTRYPOINT ["/docker-entrypoint.sh"]

EXPOSE 27040
